package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/storage"
)

// Access is what other packages need to know about a document they are allowed to touch.
type Access struct {
	ID             uuid.UUID
	SpaceID        uuid.UUID
	OwnerID        *uuid.UUID
	Title          string
	Mime           string
	Status         string
	CurrentVersion int
	Deleted        bool
}

// Access checks that p may perform act on the document and returns its basic facts.
// Documents the caller can't see are reported as not found.
func (s *Service) Access(ctx context.Context, p *auth.Principal, id uuid.UUID, act spaces.Action) (*Access, error) {
	raw, _, err := s.authorize(ctx, s.pool, p, id, act, false)
	if err != nil {
		return nil, err
	}
	return &Access{ID: raw.ID, SpaceID: raw.SpaceID, OwnerID: raw.OwnerID, Title: raw.Title, Mime: raw.MimeType,
		Status: raw.Status, CurrentVersion: raw.CurrentVersion, Deleted: raw.Deleted}, nil
}

// Hydrated loads the full document for services that already authorised access.
func (s *Service) Hydrated(ctx context.Context, id uuid.UUID) (*Document, error) {
	return s.get(ctx, id)
}

type VersionInfo struct {
	No        int       `json:"version_no"`
	Note      string    `json:"note"`
	By        *Ref      `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
	MimeType  string    `json:"mime_type"`
	Current   bool      `json:"current"`
}

// Versions lists every stored version of the document's file, newest first.
func (s *Service) Versions(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]VersionInfo, error) {
	a, err := s.Access(ctx, p, id, spaces.ActView)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT v.version_no, v.note, v.created_by, u.display_name, v.created_at, f.size_bytes, f.mime_type
		FROM document_versions v
		JOIN document_files f ON f.document_id=v.document_id AND f.kind='original' AND f.version_no=v.version_no
		LEFT JOIN users u ON u.id=v.created_by
		WHERE v.document_id=$1 ORDER BY v.version_no DESC`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (VersionInfo, error) {
		var v VersionInfo
		var by *uuid.UUID
		var name *string
		err := r.Scan(&v.No, &v.Note, &by, &name, &v.CreatedAt, &v.SizeBytes, &v.MimeType)
		if by != nil && name != nil {
			v.By = &Ref{ID: *by, Name: *name}
		}
		v.Current = v.No == a.CurrentVersion
		return v, err
	})
}

// AddVersion stores r as a new version of the document's file and re-processes it.
// Earlier versions stay available and can be restored.
func (s *Service) AddVersion(ctx context.Context, p *auth.Principal, id uuid.UUID, r io.Reader, note string) (*Document, error) {
	if _, err := s.Access(ctx, p, id, spaces.ActEdit); err != nil {
		return nil, err
	}
	st, err := s.store.Stage()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			st.Discard()
		}
	}()
	n, err := io.Copy(st, io.LimitReader(r, s.maxUpload+1))
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("receive file: %w", err))
	}
	if n > s.maxUpload {
		return nil, &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large", Msg: fmt.Sprintf("This file is larger than the %d MB limit", s.maxUpload>>20)}
	}
	if n == 0 {
		return nil, apperr.Invalid("file", "The file is empty")
	}
	mime, err := Sniff(st.Path())
	if err != nil {
		return nil, err
	}
	if mime == "" || (IsOffice(mime) && !s.OfficeEnabled()) {
		return nil, &apperr.Error{Kind: apperr.KindUnsupported, Code: "unsupported_type", Msg: "This file type isn't supported."}
	}
	sum := st.SHA256()
	key, err := st.Commit()
	if err != nil {
		return nil, err
	}
	committed = true
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.addVersionTx(ctx, tx, p, id, key, sum, mime, n, note)
	}); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// AddVersionBytes is AddVersion for in-memory results (page edits, unlocked copies).
func (s *Service) AddVersionBytes(ctx context.Context, p *auth.Principal, id uuid.UUID, b []byte, mime, note string) (*Document, error) {
	key, sum, err := storage.PutBytes(s.store, b)
	if err != nil {
		return nil, err
	}
	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, false); err != nil {
			return err
		}
		return s.addVersionTx(ctx, tx, p, id, key, sum, mime, int64(len(b)), note)
	}); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) addVersionTx(ctx context.Context, tx pgx.Tx, p *auth.Principal, id uuid.UUID, key string, sum []byte, mime string, size int64, note string) error {
	raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
	if err != nil {
		return err
	}
	if raw.Deleted {
		return apperr.Conflict("in_trash", "Restore this document from Trash before changing its file")
	}
	next := raw.CurrentVersion + 1
	if _, err := tx.Exec(ctx, `INSERT INTO document_files (id, document_id, kind, version_no, blob_key, sha256, mime_type, size_bytes)
		VALUES ($1,$2,'original',$3,$4,$5,$6,$7)`, uuid.Must(uuid.NewV7()), id, next, key, sum, mime, size); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO document_versions (document_id, version_no, note, created_by) VALUES ($1,$2,$3,$4)`,
		id, next, note, ownerOf(p)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='cancelled', updated_at=now() WHERE document_id=$1 AND status IN ('queued','leased')`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET current_version=$2, content_hash=$3, mime_type=$4, size_bytes=$5, page_count=NULL,
		status='processing', processing_stage='queued', processing_error='', version=version+1 WHERE id=$1`, id, next, sum, mime, size); err != nil {
		return err
	}
	if err := recordEvent(ctx, tx, id, p, "version_added", map[string]any{"version": next, "note": note}); err != nil {
		return err
	}
	return s.queue.InsertTx(ctx, tx, jobs.PreprocessArgs{DocumentID: id, Version: next, Priority: jobs.PriorityInteractive},
		&river.InsertOpts{Priority: jobs.PriorityInteractive, MaxAttempts: 5})
}

// RestoreVersion makes an earlier version current again by adding it as a new version
// (history is never rewritten).
func (s *Service) RestoreVersion(ctx context.Context, p *auth.Principal, id uuid.UUID, no int) (*Document, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var key, mime string
		var sum []byte
		var size int64
		err := tx.QueryRow(ctx, `SELECT blob_key, sha256, mime_type, size_bytes FROM document_files
			WHERE document_id=$1 AND kind='original' AND version_no=$2`, id, no).Scan(&key, &sum, &mime, &size)
		if db.IsNoRows(err) {
			return apperr.NotFound("Version")
		}
		if err != nil {
			return err
		}
		return s.addVersionTx(ctx, tx, p, id, key, sum, mime, size, fmt.Sprintf("Restored version %d", no))
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// OpenFileVersion opens the original file of a specific version (0 = current).
func (s *Service) OpenFileVersion(ctx context.Context, p *auth.Principal, id uuid.UUID, version int) (*File, error) {
	a, err := s.Access(ctx, p, id, spaces.ActView)
	if err != nil {
		return nil, err
	}
	if version <= 0 {
		version = a.CurrentVersion
	}
	var key, mime, orig string
	var sum []byte
	var created time.Time
	err = s.pool.QueryRow(ctx, `SELECT f.blob_key, f.mime_type, f.sha256, f.created_at, d.original_filename
		FROM document_files f JOIN documents d ON d.id=f.document_id
		WHERE f.document_id=$1 AND f.kind='original' AND f.version_no=$2`, id, version).Scan(&key, &mime, &sum, &created, &orig)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Version")
	}
	if err != nil {
		return nil, err
	}
	r, size, err := s.store.Open(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, apperr.NotFound("File")
	}
	if err != nil {
		return nil, err
	}
	name := orig
	if name == "" {
		name = a.Title + ExtFor(mime)
	} else if version != a.CurrentVersion {
		ext := filepath.Ext(name)
		name = name[:len(name)-len(ext)] + fmt.Sprintf(" (v%d)", version) + ExtFor(mime)
	}
	return &File{Reader: r, Size: size, Mime: mime, Name: name, ETag: fmt.Sprintf(`"%x"`, sum[:12]), Modified: created}, nil
}

// SetASNIfFree gives a document an archive serial number (from a scanned label) unless it
// already has one or the number is in use.
func (s *Service) SetASNIfFree(ctx context.Context, id uuid.UUID, asn int64) {
	_, _ = s.pool.Exec(ctx, `UPDATE documents SET asn=$2, version=version+1 WHERE id=$1 AND asn IS NULL
		AND NOT EXISTS (SELECT 1 FROM documents WHERE asn=$2)`, id, asn)
}

// Note adds an entry to a document's history on behalf of the system.
func (s *Service) Note(ctx context.Context, id uuid.UUID, action string, details map[string]any) {
	_ = RecordEvent(ctx, s.pool, id, action, details)
}

// Principal returns who an automatic action on this document acts as: its uploader while
// they are an active user (so new documents belong to them), otherwise the system.
func (s *Service) Principal(ctx context.Context, id uuid.UUID) *auth.Principal {
	var uid uuid.UUID
	var name, email string
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.display_name, u.email FROM documents d JOIN users u ON u.id=d.owner_id
		WHERE d.id=$1 AND u.status='active'`, id).Scan(&uid, &name, &email)
	if err != nil {
		return auth.System()
	}
	return &auth.Principal{Kind: auth.KindSession, UserID: uid, Name: name, Email: email}
}

// FinishSplit marks a split-up original as done and moves it to the Trash.
func (s *Service) FinishSplit(ctx context.Context, id uuid.UUID, parts int) error {
	if _, err := s.pool.Exec(ctx, `UPDATE documents SET status='ready', processing_stage='done', inbox=false WHERE id=$1`, id); err != nil {
		return err
	}
	s.Note(ctx, id, "split_on_separators", map[string]any{"parts": parts})
	return s.Trash(ctx, auth.System(), id)
}
