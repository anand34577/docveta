package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/storage"
	"github.com/anand34577/docveta/internal/taxonomy"
	"github.com/anand34577/docveta/internal/textindex"
)

// ---------------------------------------------------------------------------
// OCR finalize: merge task results into pages, make the run current, index.
// ---------------------------------------------------------------------------

type FinalizeWorker struct {
	river.WorkerDefaults[jobs.OCRFinalizeArgs]
	S *Service
}

func (w *FinalizeWorker) Timeout(*river.Job[jobs.OCRFinalizeArgs]) time.Duration {
	return 10 * time.Minute
}

func (w *FinalizeWorker) Work(ctx context.Context, job *river.Job[jobs.OCRFinalizeArgs]) error {
	return w.S.finalize(ctx, job.Args.RunID)
}

// maybeFinalizeTx enqueues finalize when no task of the run is still pending.
func (s *Service) maybeFinalizeTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	var pending bool
	// Lock the run row so concurrent completions of the last two tasks can't both miss it.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM ocr_runs WHERE id=$1 FOR UPDATE`, runID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM processing_tasks WHERE ocr_run_id=$1 AND type IN ('ocr','embedded')
		AND status IN ('queued','leased'))`, runID).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return nil
	}
	// No unique-job dedupe: finalize is idempotent (it ignores runs that are no longer
	// open), and deduping by run dropped the finalize after an admin retry of a run
	// that had already failed, leaving the document stuck in processing.
	return s.queue.InsertTx(ctx, tx, jobs.OCRFinalizeArgs{RunID: runID}, &river.InsertOpts{
		Priority: jobs.PriorityNormal, MaxAttempts: 10,
	})
}

func (s *Service) finalize(ctx context.Context, runID uuid.UUID) error {
	cfg := loadSettings(ctx, s.settings)
	var notifyFail *docRow
	var failMsg string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var docID uuid.UUID
		var status string
		var version int
		err := tx.QueryRow(ctx, `SELECT document_id, status, version_no FROM ocr_runs WHERE id=$1 FOR UPDATE`, runID).Scan(&docID, &status, &version)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if status != "running" && status != "pending" {
			return nil // already finalized or cancelled
		}
		d, err := s.loadDoc(ctx, tx, docID)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Deleted || d.CurrentVersion != version {
			_, err := tx.Exec(ctx, `UPDATE ocr_runs SET status='cancelled', finished_at=now() WHERE id=$1`, runID)
			return err
		}

		rows, err := tx.Query(ctx, `SELECT type, status, result_blob_key, last_error FROM processing_tasks
			WHERE ocr_run_id=$1 AND type IN ('ocr','embedded') ORDER BY page_from NULLS FIRST`, runID)
		if err != nil {
			return err
		}
		type taskRow struct {
			typ, status, lastErr string
			key                  *string
		}
		var tasks []taskRow
		for rows.Next() {
			var t taskRow
			if err := rows.Scan(&t.typ, &t.status, &t.key, &t.lastErr); err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, t := range tasks {
			if t.status == "queued" || t.status == "leased" {
				return nil // not complete yet; a later completion re-triggers finalize
			}
		}

		merged := OCRResult{Schema: SchemaV1}
		pages := map[int]OCRPage{}
		engines := []string{}
		var ocrTasks, ocrFailed int
		var lastErr string
		for _, t := range tasks {
			if t.typ == "ocr" {
				ocrTasks++
			}
			if t.status != "done" || t.key == nil {
				if t.typ == "ocr" && t.status == "failed" {
					ocrFailed++
					lastErr = t.lastErr
				}
				continue
			}
			raw, err := storage.ReadAll(ctx, s.store, *t.key, 256<<20)
			if err != nil {
				return fmt.Errorf("read task result: %w", err)
			}
			res, err := ParseOCRResult(raw, 0, 0)
			if err != nil {
				s.log.Error("stored OCR result invalid", "run", runID, "err", err)
				continue
			}
			if !containsStr(engines, res.Engine.Name) {
				engines = append(engines, res.Engine.Name)
			}
			if t.typ == "ocr" && merged.Engine.Name == "" {
				merged.Engine = res.Engine
			}
			for _, p := range res.Pages {
				pages[p.Page] = p
			}
		}
		if merged.Engine.Name == "" {
			merged.Engine = OCREngine{Name: "embedded", Version: "pdfium"}
		}
		var confSum float64
		var confN int
		for _, no := range sortedPageKeys(pages) {
			p := pages[no]
			merged.Pages = append(merged.Pages, p)
			if p.Confidence != nil {
				confSum += *p.Confidence
				confN++
			}
		}
		hasBoxes := merged.HasBoxes()

		runStatus := "done"
		if ocrTasks > 0 && ocrFailed == ocrTasks && len(merged.Pages) == 0 {
			runStatus = "failed"
		}
		raw, _ := json.Marshal(merged)
		resultKey, _, err := storage.PutBytes(s.store, raw)
		if err != nil {
			return err
		}
		var avg any
		if confN > 0 {
			avg = confSum / float64(confN)
		}

		if runStatus == "done" {
			if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET is_current=false WHERE document_id=$1 AND is_current`, docID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET status=$2, engine=$3, engine_version=$4, pages_done=$5, avg_confidence=$6,
			has_boxes=$7, is_current=($2='done'), result_blob_key=$8, error=$9, finished_at=now() WHERE id=$1`,
			runID, runStatus, strings.Join(engines, "+"), merged.Engine.Version, len(merged.Pages), avg, hasBoxes, resultKey, lastErr); err != nil {
			return err
		}

		if runStatus == "done" {
			if _, err := tx.Exec(ctx, `DELETE FROM pages WHERE document_id=$1`, docID); err != nil {
				return err
			}
			maxPage := 0
			for _, p := range merged.Pages {
				var conf any
				if p.Confidence != nil {
					conf = *p.Confidence
				}
				if _, err := tx.Exec(ctx, `INSERT INTO pages (document_id, page_no, ocr_run_id, text, confidence, rotation)
					VALUES ($1,$2,$3,$4,$5,$6)`, docID, p.Page, runID, textindex.Normalize(p.Text), conf, p.Rotation); err != nil {
					return err
				}
				maxPage = max(maxPage, p.Page)
			}
			if maxPage > 0 {
				if _, err := tx.Exec(ctx, `UPDATE documents SET page_count=greatest(coalesce(page_count,0), $2) WHERE id=$1`, docID, maxPage); err != nil {
					return err
				}
			}
			if err := documents.ReindexContent(ctx, tx, docID); err != nil {
				return err
			}
			if cfg.Archive && hasBoxes && (d.Mime == documents.MimePDF || documents.IsImage(d.Mime) || documents.NeedsWorkerConversion(d.Mime)) {
				payload, _ := json.Marshal(map[string]any{"ocr_result_key": resultKey})
				// The archive worker receives the working copy (e.g. HEIC converted to JPEG), so route by its type.
				workMime := d.Mime
				_ = tx.QueryRow(ctx, `SELECT mime_type FROM document_files WHERE document_id=$1 AND kind='derived' AND version_no=$2`,
					docID, d.CurrentVersion).Scan(&workMime)
				if _, err := tx.Exec(ctx, `INSERT INTO processing_tasks (id, document_id, ocr_run_id, type, mime_type, payload, priority, max_attempts)
					VALUES ($1,$2,$3,'archive',$4,$5,$6,$7)`, uuid.Must(uuid.NewV7()), docID, runID, workMime, payload, jobs.PriorityBulk, cfg.MaxAttempts); err != nil {
					return err
				}
				defer s.signal()
			}
		}

		msg := ""
		if ocrFailed > 0 {
			msg = "Text recognition failed"
			if ocrFailed < ocrTasks {
				msg += " for some pages"
			}
			if lastErr != "" {
				msg += ": " + lastErr
			}
		}
		if runStatus == "failed" {
			notifyFail, failMsg = d, msg
			if err := s.setStatus(ctx, tx, docID, "failed", "failed", msg); err != nil {
				return err
			}
			return documents.RecordEvent(ctx, tx, docID, "processing_failed", map[string]any{"error": msg})
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET processing_stage='classifying', processing_error=$2 WHERE id=$1`, docID, msg); err != nil {
			return err
		}
		if err := documents.RecordEvent(ctx, tx, docID, "text_extracted", map[string]any{
			"engine": strings.Join(engines, "+"), "pages": len(merged.Pages), "avg_confidence": avg}); err != nil {
			return err
		}
		return s.queue.InsertTx(ctx, tx, jobs.ClassifyArgs{DocumentID: docID, Finalize: true}, &river.InsertOpts{Priority: jobs.PriorityNormal, MaxAttempts: 5})
	})
	if err != nil {
		return err
	}
	if notifyFail != nil && notifyFail.OwnerID != nil {
		id := notifyFail.ID
		_ = s.queue.Emit(ctx, jobs.Event{Type: "document.failed", Title: "Couldn't read text in \"" + notifyFail.Title + "\"",
			Body: failMsg, Link: "/documents/" + id.String(), Severity: "error", Recipients: []uuid.UUID{*notifyFail.OwnerID}, DocumentID: &id})
	}
	return nil
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func sortedPageKeys(m map[int]OCRPage) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// ---------------------------------------------------------------------------
// Classify: rules + date extraction (+ AI later). Never overrides human edits.
// ---------------------------------------------------------------------------

type ClassifyWorker struct {
	river.WorkerDefaults[jobs.ClassifyArgs]
	S *Service
}

func (w *ClassifyWorker) Work(ctx context.Context, job *river.Job[jobs.ClassifyArgs]) error {
	return w.S.classify(ctx, job.Args)
}

type ruleItem struct {
	id   uuid.UUID
	name string
	rule taxonomy.Rule
}

func loadRules(ctx context.Context, q db.Querier, table string, spaceID uuid.UUID) ([]ruleItem, error) {
	rows, err := q.Query(ctx, `SELECT id, name, match_algorithm, match_pattern, case_sensitive FROM `+table+`
		WHERE space_id=$1 AND match_algorithm <> 'none' AND match_pattern <> '' ORDER BY length(match_pattern) DESC, lower(name)`, spaceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ruleItem, error) {
		var it ruleItem
		err := r.Scan(&it.id, &it.name, &it.rule.Algorithm, &it.rule.Pattern, &it.rule.CaseSensitive)
		return it, err
	})
}

func (s *Service) classify(ctx context.Context, a jobs.ClassifyArgs) error {
	var notify *jobs.Event
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var spaceID uuid.UUID
		var ownerID *uuid.UUID
		var title, content, status, dateFormat string
		var corr, typ *uuid.UUID
		var docDate *time.Time
		var deleted bool
		err := tx.QueryRow(ctx, `SELECT d.space_id, d.owner_id, d.title, d.content, d.status, d.correspondent_id, d.document_type_id,
			d.document_date, d.deleted_at IS NOT NULL, coalesce(u.date_format, 'DD/MM/YYYY')
			FROM documents d LEFT JOIN users u ON u.id=d.owner_id WHERE d.id=$1 FOR UPDATE OF d`, a.DocumentID).
			Scan(&spaceID, &ownerID, &title, &content, &status, &corr, &typ, &docDate, &deleted, &dateFormat)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if deleted {
			return nil
		}
		userSet := map[string]bool{}
		rows, err := tx.Query(ctx, `SELECT field FROM field_sources WHERE document_id=$1 AND source='user'`, a.DocumentID)
		if err != nil {
			return err
		}
		fields, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, f := range fields {
			userSet[f] = true
		}

		mt := taxonomy.NewMatchText(title + "\n" + content)
		applied := map[string]any{}

		tags, err := loadRules(ctx, tx, "tags", spaceID)
		if err != nil {
			return err
		}
		var added []string
		for _, t := range tags {
			if t.rule.Matches(mt) {
				tag, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id, source) VALUES ($1,$2,'rule') ON CONFLICT DO NOTHING`, a.DocumentID, t.id)
				if err != nil {
					return err
				}
				if tag.RowsAffected() > 0 {
					added = append(added, t.name)
				}
			}
		}
		if len(added) > 0 {
			applied["tags"] = added
		}
		if corr == nil && !userSet["correspondent"] {
			items, err := loadRules(ctx, tx, "correspondents", spaceID)
			if err != nil {
				return err
			}
			for _, it := range items {
				if it.rule.Matches(mt) {
					if _, err := tx.Exec(ctx, `UPDATE documents SET correspondent_id=$2 WHERE id=$1`, a.DocumentID, it.id); err != nil {
						return err
					}
					if err := setRuleSource(ctx, tx, a.DocumentID, "correspondent"); err != nil {
						return err
					}
					applied["correspondent"] = it.name
					break
				}
			}
		}
		if typ == nil && !userSet["document_type"] {
			items, err := loadRules(ctx, tx, "document_types", spaceID)
			if err != nil {
				return err
			}
			for _, it := range items {
				if it.rule.Matches(mt) {
					if _, err := tx.Exec(ctx, `UPDATE documents SET document_type_id=$2 WHERE id=$1`, a.DocumentID, it.id); err != nil {
						return err
					}
					if err := setRuleSource(ctx, tx, a.DocumentID, "document_type"); err != nil {
						return err
					}
					applied["document_type"] = it.name
					break
				}
			}
		}
		if docDate == nil && !userSet["document_date"] && content != "" {
			dayFirst := !strings.HasPrefix(dateFormat, "MM")
			if t, ok := ExtractDate(content, dayFirst, s.now()); ok {
				if _, err := tx.Exec(ctx, `UPDATE documents SET document_date=$2 WHERE id=$1`, a.DocumentID, t.Format("2006-01-02")); err != nil {
					return err
				}
				if err := setRuleSource(ctx, tx, a.DocumentID, "document_date"); err != nil {
					return err
				}
				applied["document_date"] = t.Format("2006-01-02")
			}
		}
		if len(applied) > 0 {
			if err := documents.RecordEvent(ctx, tx, a.DocumentID, "auto_classified", applied); err != nil {
				return err
			}
			if err := documents.ReindexMeta(ctx, tx, a.DocumentID); err != nil {
				return err
			}
		}
		if !a.Finalize {
			return nil
		}
		if status == "processing" {
			if _, err := tx.Exec(ctx, `UPDATE documents SET status='ready', processing_stage='done' WHERE id=$1`, a.DocumentID); err != nil {
				return err
			}
			var wf bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflows WHERE space_id=$1 AND enabled AND trigger='processed')`, spaceID).Scan(&wf); err == nil && wf {
				if err := s.queue.InsertTx(ctx, tx, jobs.WorkflowArgs{DocumentID: a.DocumentID, Trigger: "processed"},
					&river.InsertOpts{Priority: jobs.PriorityNormal, MaxAttempts: 3}); err != nil {
					return err
				}
			}
			// Optional AI step (suggestions, embeddings): only when a provider is set up and the
			// space allows it, and always after the document is already usable.
			var ai bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ai_providers WHERE enabled) AND
				(SELECT ai_policy <> 'off' FROM spaces WHERE id=$1)`, spaceID).Scan(&ai); err == nil && ai {
				if err := s.queue.InsertTx(ctx, tx, jobs.AIArgs{DocumentID: a.DocumentID, Classify: true, Embed: true},
					&river.InsertOpts{Priority: jobs.PriorityBackground, MaxAttempts: 2}); err != nil {
					return err
				}
			}
		}
		if ownerID != nil {
			id := a.DocumentID
			notify = &jobs.Event{Type: "document.processed", Title: "\"" + title + "\" is ready",
				Body: "It's in your Inbox for a quick review.", Link: "/documents/" + id.String(), Severity: "success",
				Recipients: []uuid.UUID{*ownerID}, DocumentID: &id, SpaceID: &spaceID}
			return s.queue.EmitTx(ctx, tx, *notify)
		}
		return nil
	})
	return err
}

func setRuleSource(ctx context.Context, tx pgx.Tx, docID uuid.UUID, field string) error {
	_, err := tx.Exec(ctx, `INSERT INTO field_sources (document_id, field, source) VALUES ($1,$2,'rule')
		ON CONFLICT (document_id, field) DO UPDATE SET source='rule', set_at=now() WHERE field_sources.source <> 'user'`, docID, field)
	return err
}

// ---------------------------------------------------------------------------
// Reindex metadata in the background (after taxonomy renames/merges).
// ---------------------------------------------------------------------------

type ReindexWorker struct {
	river.WorkerDefaults[jobs.ReindexArgs]
	S *Service
}

func (w *ReindexWorker) Work(ctx context.Context, job *river.Job[jobs.ReindexArgs]) error {
	for _, id := range job.Args.DocumentIDs {
		if err := documents.ReindexMeta(ctx, w.S.pool, id); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueReindex schedules metadata reindexing for documents, in chunks.
func (s *Service) EnqueueReindex(ctx context.Context, ids []uuid.UUID) error {
	for i := 0; i < len(ids); i += 500 {
		end := min(i+500, len(ids))
		if err := s.queue.Insert(ctx, jobs.ReindexArgs{DocumentIDs: ids[i:end]}, &river.InsertOpts{Priority: jobs.PriorityBackground}); err != nil {
			return err
		}
	}
	return nil
}
