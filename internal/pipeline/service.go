package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/settings"
	"github.com/anand34577/docveta/internal/storage"
)

type Service struct {
	pool     *pgxpool.Pool
	store    storage.Store
	pdf      *PDF
	queue    *jobs.Queue
	settings *settings.Store
	log      *slog.Logger
	now      func() time.Time

	// wake notifies long-polling workers that new tasks may be available.
	wakeMu sync.Mutex
	wake   chan struct{}
}

func NewService(pool *pgxpool.Pool, store storage.Store, pdf *PDF, q *jobs.Queue, st *settings.Store, log *slog.Logger) *Service {
	return &Service{pool: pool, store: store, pdf: pdf, queue: q, settings: st, log: log, now: time.Now, wake: make(chan struct{})}
}

// signal wakes all long-polling lease requests.
func (s *Service) signal() {
	s.wakeMu.Lock()
	close(s.wake)
	s.wake = make(chan struct{})
	s.wakeMu.Unlock()
}

func (s *Service) waitCh() <-chan struct{} {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.wake
}

// maxTextPages bounds embedded-text extraction for pathological PDFs.
const maxTextPages = 5000

type docRow struct {
	ID             uuid.UUID
	SpaceID        uuid.UUID
	OwnerID        *uuid.UUID
	Mime           string
	Language       string
	Title          string
	CurrentVersion int
	Deleted        bool
	BlobKey        string
	Size           int64
	Source         string
}

func (s *Service) loadDoc(ctx context.Context, q db.Querier, id uuid.UUID) (*docRow, error) {
	var d docRow
	err := q.QueryRow(ctx, `SELECT d.id, d.space_id, d.owner_id, d.mime_type, d.language, d.title, d.current_version,
		d.deleted_at IS NOT NULL, f.blob_key, f.size_bytes, d.source
		FROM documents d JOIN document_files f ON f.document_id=d.id AND f.kind='original' AND f.version_no=d.current_version
		WHERE d.id=$1`, id).Scan(&d.ID, &d.SpaceID, &d.OwnerID, &d.Mime, &d.Language, &d.Title, &d.CurrentVersion, &d.Deleted,
		&d.BlobKey, &d.Size, &d.Source)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// Preprocess (runs in the core)
// ---------------------------------------------------------------------------

type PreprocessWorker struct {
	river.WorkerDefaults[jobs.PreprocessArgs]
	S *Service
}

func (w *PreprocessWorker) Timeout(*river.Job[jobs.PreprocessArgs]) time.Duration {
	return 15 * time.Minute
}

func (w *PreprocessWorker) Work(ctx context.Context, job *river.Job[jobs.PreprocessArgs]) error {
	return w.S.preprocess(ctx, job.Args)
}

func (s *Service) setStatus(ctx context.Context, q db.Querier, id uuid.UUID, status, stage, msg string) error {
	_, err := q.Exec(ctx, `UPDATE documents SET status=$2, processing_stage=$3, processing_error=$4 WHERE id=$1`, id, status, stage, msg)
	return err
}

func (s *Service) preprocess(ctx context.Context, a jobs.PreprocessArgs) error {
	d, err := s.loadDoc(ctx, s.pool, a.DocumentID)
	if db.IsNoRows(err) {
		return nil // deleted meanwhile
	}
	if err != nil {
		return err
	}
	if d.Deleted || d.CurrentVersion != a.Version {
		return nil
	}
	cfg := loadSettings(ctx, s.settings)
	forceOCR := a.Profile == "force-ocr"

	r, size, err := s.store.Open(ctx, d.BlobKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return s.fail(ctx, d, "The original file is missing from storage. Restore it from a backup.")
		}
		return err
	}
	defer r.Close()

	var (
		pageCount   int
		embedded    = map[int]string{} // page -> text
		ocrPages    []int              // 1-based pages needing OCR
		ocrAll      bool               // OCR the whole file (images, HEIC): page count known only to the worker
		thumb       []byte
		thumbW      int
		thumbH      int
		thumbFailed bool
	)

	switch {
	case d.Mime == documents.MimePDF:
		info, err := s.pdf.Inspect(r, size, "", maxTextPages)
		if errors.Is(err, ErrPassword) {
			return s.setStatus(ctx, s.pool, d.ID, "needs_password", "needs_password",
				"This PDF is password protected. Docveta can't read it until it's unlocked.")
		}
		if err != nil {
			s.log.Warn("pdf inspect failed", "document", d.ID, "err", err)
			return s.fail(ctx, d, "This PDF appears to be damaged and couldn't be read. You can still download it.")
		}
		pageCount = info.PageCount
		for i, t := range info.Texts {
			if !forceOCR && cfg.SkipOCRWithText && HasUsableText(t) {
				embedded[i+1] = t
			} else {
				ocrPages = append(ocrPages, i+1)
			}
		}
		if info.Thumbnail != nil {
			thumb, thumbW, thumbH, err = Thumbnail(info.Thumbnail, 1)
			thumbFailed = err != nil
		}
	case documents.IsImage(d.Mime):
		img, orientation, err := DecodeImage(r)
		if err != nil {
			s.log.Warn("image decode failed", "document", d.ID, "err", err)
			thumbFailed = true
		} else {
			thumb, thumbW, thumbH, err = Thumbnail(img, orientation)
			thumbFailed = err != nil
		}
		pageCount = 1
		ocrAll = true
		if d.Mime == documents.MimeTIFF {
			pageCount = 0 // multi-page TIFF: worker reports pages
		}
	case documents.NeedsWorkerConversion(d.Mime):
		ocrAll = true
	case d.Mime == documents.MimeText:
		b, err := io.ReadAll(io.LimitReader(r, 8<<20))
		if err != nil {
			return err
		}
		pageCount = 1
		embedded[1] = string(b)
	default:
		return s.fail(ctx, d, "This file type can't be processed yet.")
	}
	if thumbFailed {
		s.log.Warn("thumbnail failed", "document", d.ID)
	}

	runID := uuid.Must(uuid.NewV7())
	needOCR := ocrAll || len(ocrPages) > 0
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Re-check under lock: the document may have been trashed or reprocessed meanwhile.
		var deleted bool
		var version int
		if err := tx.QueryRow(ctx, `SELECT deleted_at IS NOT NULL, current_version FROM documents WHERE id=$1 FOR UPDATE`, d.ID).Scan(&deleted, &version); err != nil {
			return err
		}
		if deleted || version != a.Version {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='cancelled', updated_at=now()
			WHERE document_id=$1 AND status IN ('queued','leased')`, d.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET status='cancelled' WHERE document_id=$1 AND status IN ('pending','running')`, d.ID); err != nil {
			return err
		}
		if thumb != nil {
			key, sum, err := storage.PutBytes(s.store, thumb)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO document_files (id, document_id, kind, version_no, blob_key, sha256, mime_type, size_bytes, width, height)
				VALUES ($1,$2,'thumbnail',$3,$4,$5,'image/jpeg',$6,$7,$8)
				ON CONFLICT (document_id, kind, version_no) DO UPDATE SET blob_key=excluded.blob_key, sha256=excluded.sha256,
					size_bytes=excluded.size_bytes, width=excluded.width, height=excluded.height, created_at=now()`,
				uuid.Must(uuid.NewV7()), d.ID, version, key, sum, len(thumb), thumbW, thumbH); err != nil {
				return err
			}
		}
		var pc any
		if pageCount > 0 {
			pc = pageCount
		}
		stage := "indexing"
		if needOCR {
			stage = "awaiting_ocr"
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET page_count=coalesce($2, page_count), status='processing', processing_stage=$3,
			processing_error='' WHERE id=$1`, d.ID, pc, stage); err != nil {
			return err
		}
		pagesTotal := len(ocrPages) + len(embedded)
		if _, err := tx.Exec(ctx, `INSERT INTO ocr_runs (id, document_id, version_no, profile, status, pages_total, started_at)
			VALUES ($1,$2,$3,$4,'running',$5,now())`, runID, d.ID, version, coalesce(a.Profile, "default"), pagesTotal); err != nil {
			return err
		}
		if len(embedded) > 0 {
			res := OCRResult{Schema: SchemaV1, Engine: OCREngine{Name: "embedded", Version: "pdfium"}}
			for _, p := range sortedKeys(embedded) {
				res.Pages = append(res.Pages, OCRPage{Page: p, Text: embedded[p]})
			}
			raw, _ := json.Marshal(res)
			key, _, err := storage.PutBytes(s.store, raw)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO processing_tasks (id, document_id, ocr_run_id, type, mime_type, status, result_blob_key, finished_at)
				VALUES ($1,$2,$3,'embedded',$4,'done',$5,now())`, uuid.Must(uuid.NewV7()), d.ID, runID, d.Mime, key); err != nil {
				return err
			}
		}
		if needOCR {
			priority := a.Priority
			if priority == 0 {
				priority = jobs.PriorityNormal
			}
			if err := s.createOCRTasks(ctx, tx, d, runID, ocrPages, ocrAll, priority, cfg); err != nil {
				return err
			}
			return documents.RecordEvent(ctx, tx, d.ID, "ocr_queued", map[string]any{"pages": len(ocrPages), "embedded_pages": len(embedded)})
		}
		return s.queue.InsertTx(ctx, tx, jobs.OCRFinalizeArgs{RunID: runID}, &river.InsertOpts{Priority: priority(a), MaxAttempts: 10})
	})
	if err != nil {
		return err
	}
	if needOCR {
		s.signal()
	}
	return nil
}

func priority(a jobs.PreprocessArgs) int {
	if a.Priority == 0 {
		return jobs.PriorityNormal
	}
	return a.Priority
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func sortedKeys(m map[int]string) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// fail marks a document as failed (it stays viewable/downloadable) and notifies the owner.
func (s *Service) fail(ctx context.Context, d *docRow, msg string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.setStatus(ctx, tx, d.ID, "failed", "failed", msg); err != nil {
			return err
		}
		if err := documents.RecordEvent(ctx, tx, d.ID, "processing_failed", map[string]any{"error": msg}); err != nil {
			return err
		}
		if d.OwnerID == nil {
			return nil
		}
		id := d.ID
		return s.queue.EmitTx(ctx, tx, jobs.Event{Type: "document.failed", Title: "Couldn't process \"" + d.Title + "\"",
			Body: msg, Link: "/documents/" + d.ID.String(), Severity: "error", Recipients: []uuid.UUID{*d.OwnerID}, DocumentID: &id})
	})
}

// ocrLanguage maps a document language (BCP 47) to the code workers advertise (ISO 639-1/3).
func ocrLanguage(lang string) string {
	l := strings.ToLower(strings.SplitN(lang, "-", 2)[0])
	if l == "" {
		return "en"
	}
	return l
}

// createOCRTasks splits OCR work into page batches and applies routing (DESIGN §11.6).
func (s *Service) createOCRTasks(ctx context.Context, tx pgx.Tx, d *docRow, runID uuid.UUID, pages []int, all bool, priority int, cfg Settings) error {
	langs := []string{ocrLanguage(d.Language)}
	tags := cfg.PreferTags
	fallbackAt := s.now().Add(time.Duration(cfg.FallbackAfterMinutes) * time.Minute)
	if len(tags) > 0 {
		// If no recently seen worker has the preferred tags, don't make users wait.
		var any bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workers w, jsonb_array_elements(w.capabilities) c
			WHERE w.enabled AND w.last_seen_at > now() - interval '1 day' AND c->>'task_type'='ocr'
			  AND (SELECT coalesce(array_agg(x), '{}') FROM jsonb_array_elements_text(c->'tags') x) @> $1)`, tags).Scan(&any); err != nil {
			return err
		}
		if !any {
			fallbackAt = s.now()
		}
	} else {
		tags = []string{}
	}
	insert := func(from, to *int) error {
		_, err := tx.Exec(ctx, `INSERT INTO processing_tasks (id, document_id, ocr_run_id, type, mime_type, page_from, page_to,
			required_languages, required_tags, fallback_at, priority, max_attempts)
			VALUES ($1,$2,$3,'ocr',$4,$5,$6,$7,$8,$9,$10,$11)`,
			uuid.Must(uuid.NewV7()), d.ID, runID, d.Mime, from, to, langs, tags, fallbackAt, priority, cfg.MaxAttempts)
		return err
	}
	if all {
		return insert(nil, nil)
	}
	// Group consecutive pages into batches of at most PageBatchSize.
	for i := 0; i < len(pages); {
		j := i
		for j+1 < len(pages) && pages[j+1] == pages[j]+1 && j+1-i < cfg.PageBatchSize {
			j++
		}
		from, to := pages[i], pages[j]
		if err := insert(&from, &to); err != nil {
			return err
		}
		i = j + 1
	}
	return nil
}
