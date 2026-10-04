// Package folders imports files dropped into watched folders (a scanner's SMB share, a
// synced directory): each stable file becomes a document, then moves to done/ (or is
// deleted); files that can't be imported move to failed/ with an explanation.
package folders

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/taxonomy"
)

type Folder struct {
	ID            uuid.UUID   `json:"id"`
	Path          string      `json:"path"`
	SpaceID       uuid.UUID   `json:"space_id"`
	Enabled       bool        `json:"enabled"`
	Recursive     bool        `json:"recursive"`
	Subfolders    string      `json:"subfolders"` // none | tag | space
	TagIDs        []uuid.UUID `json:"tag_ids"`
	AfterImport   string      `json:"after_import"` // move | delete
	StableSeconds int         `json:"stable_seconds"`
	LastScanAt    *time.Time  `json:"last_scan_at"`
	LastError     string      `json:"last_error"`
	ImportedCount int         `json:"imported_count"`
	FailedCount   int         `json:"failed_count"`
}

type Input struct {
	Path          *string      `json:"path"`
	SpaceID       *uuid.UUID   `json:"space_id"`
	Enabled       *bool        `json:"enabled"`
	Recursive     *bool        `json:"recursive"`
	Subfolders    *string      `json:"subfolders"`
	TagIDs        *[]uuid.UUID `json:"tag_ids"`
	AfterImport   *string      `json:"after_import"`
	StableSeconds *int         `json:"stable_seconds"`
}

type Service struct {
	pool  *pgxpool.Pool
	docs  *documents.Service
	queue *jobs.Queue
	audit *audit.Log
	log   *slog.Logger
	roots []string

	mu   sync.Mutex
	seen map[string]observed // last size and modification time per file, to wait until copying stops
}

type observed struct {
	size int64
	mod  time.Time
}

// NewService limits watched folders to the given roots (default: <data dir>/watch).
func NewService(pool *pgxpool.Pool, docs *documents.Service, q *jobs.Queue, al *audit.Log, log *slog.Logger, dataDir string, roots []string) *Service {
	if len(roots) == 0 {
		roots = []string{filepath.Join(dataDir, "watch")}
	}
	var clean []string
	for _, r := range roots {
		if abs, err := filepath.Abs(r); err == nil {
			if real, err := filepath.EvalSymlinks(abs); err == nil {
				abs = real
			}
			clean = append(clean, abs)
		}
	}
	return &Service{pool: pool, docs: docs, queue: q, audit: al, log: log, roots: clean, seen: map[string]observed{}}
}

// Roots lists the directories folders may live under.
func (s *Service) Roots() []string { return s.roots }

// resolve turns a path from the admin into a clean absolute path inside an allowed root.
func (s *Service) resolve(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", apperr.Invalid("path", "Enter a folder path")
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(s.roots[0], raw) // a bare name means a folder inside the first root
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", apperr.Invalid("path", "That isn't a valid path")
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return "", apperr.Invalid("path", "Can't create or open that folder: "+err.Error())
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", apperr.Invalid("path", "Can't open that folder")
	}
	for _, root := range s.roots {
		if rel, err := filepath.Rel(root, real); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return real, nil
		}
	}
	return "", apperr.Invalid("path", "Folders must be inside: "+strings.Join(s.roots, ", ")+". An administrator can add more places with DOCVETA_WATCH_ROOTS.")
}

func (in *Input) validate() error {
	var v apperr.Validation
	if in.Subfolders != nil && !slices.Contains([]string{"none", "tag", "space"}, *in.Subfolders) {
		v.Add("subfolders", "Must be none, tag or space")
	}
	if in.AfterImport != nil && !slices.Contains([]string{"move", "delete"}, *in.AfterImport) {
		v.Add("after_import", "Must be move or delete")
	}
	if in.StableSeconds != nil && (*in.StableSeconds < 0 || *in.StableSeconds > 3600) {
		v.Add("stable_seconds", "Between 0 and 3600")
	}
	return v.Err()
}

const cols = `id, path, space_id, enabled, recursive, subfolders, tag_ids, after_import, stable_seconds, last_scan_at, last_error, imported_count, failed_count`

func scan(row pgx.Row) (*Folder, error) {
	var f Folder
	if err := row.Scan(&f.ID, &f.Path, &f.SpaceID, &f.Enabled, &f.Recursive, &f.Subfolders, &f.TagIDs, &f.AfterImport, &f.StableSeconds,
		&f.LastScanAt, &f.LastError, &f.ImportedCount, &f.FailedCount); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Folder")
		}
		return nil, err
	}
	if f.TagIDs == nil {
		f.TagIDs = []uuid.UUID{}
	}
	return &f, nil
}

func (s *Service) List(ctx context.Context, p *auth.Principal) ([]*Folder, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM watched_folders ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Folder{}
	for rows.Next() {
		f, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, in Input) (*Folder, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if in.Path == nil {
		return nil, apperr.Invalid("path", "Enter a folder path")
	}
	if in.SpaceID == nil {
		return nil, apperr.Invalid("space_id", "Choose the space the documents go to")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	path, err := s.resolve(*in.Path)
	if err != nil {
		return nil, err
	}
	var kind string
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM spaces WHERE id=$1`, *in.SpaceID).Scan(&kind); err != nil {
		return nil, apperr.Invalid("space_id", "That space doesn't exist")
	}
	id := uuid.Must(uuid.NewV7())
	tags := []uuid.UUID{}
	if in.TagIDs != nil {
		tags = *in.TagIDs
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO watched_folders (id, path, space_id, created_by, enabled, recursive, subfolders, tag_ids, after_import, stable_seconds)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, path, *in.SpaceID, p.UserID, deref(in.Enabled, true), deref(in.Recursive, true),
		deref(in.Subfolders, "none"), tags, deref(in.AfterImport, "move"), deref(in.StableSeconds, 10))
	if db.IsUniqueViolation(err) {
		return nil, apperr.Conflict("path_taken", "This folder is already being watched")
	}
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "folder.create", "watched_folder", id.String(), map[string]any{"path": path})
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM watched_folders WHERE id=$1`, id))
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*Folder, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	var path *string
	if in.Path != nil {
		r, err := s.resolve(*in.Path)
		if err != nil {
			return nil, err
		}
		path = &r
	}
	var tags []uuid.UUID
	if in.TagIDs != nil {
		tags = *in.TagIDs
	}
	tag, err := s.pool.Exec(ctx, `UPDATE watched_folders SET path=coalesce($2,path), space_id=coalesce($3,space_id), enabled=coalesce($4,enabled),
		recursive=coalesce($5,recursive), subfolders=coalesce($6,subfolders), tag_ids=CASE WHEN $7 THEN $8 ELSE tag_ids END,
		after_import=coalesce($9,after_import), stable_seconds=coalesce($10,stable_seconds) WHERE id=$1`,
		id, path, in.SpaceID, in.Enabled, in.Recursive, in.Subfolders, in.TagIDs != nil, tags, in.AfterImport, in.StableSeconds)
	if db.IsUniqueViolation(err) {
		return nil, apperr.Conflict("path_taken", "This folder is already being watched")
	}
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperr.NotFound("Folder")
	}
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM watched_folders WHERE id=$1`, id))
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM watched_folders WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Folder")
	}
	return nil
}

// ScanResult summarises one pass.
type ScanResult struct {
	Imported int `json:"imported"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"` // not stable yet, or ignored
}

// ScanNow scans one folder immediately (admin action; also used by tests).
func (s *Service) ScanNow(ctx context.Context, p *auth.Principal, id uuid.UUID) (*ScanResult, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	f, err := scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM watched_folders WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	return s.scanFolder(ctx, f)
}

type ScanWorker struct {
	river.WorkerDefaults[jobs.FolderScanArgs]
	S *Service
}

func (w *ScanWorker) Timeout(*river.Job[jobs.FolderScanArgs]) time.Duration { return 30 * time.Minute }

func (w *ScanWorker) Work(ctx context.Context, _ *river.Job[jobs.FolderScanArgs]) error {
	rows, err := w.S.pool.Query(ctx, `SELECT `+cols+` FROM watched_folders WHERE enabled`)
	if err != nil {
		return err
	}
	var list []*Folder
	for rows.Next() {
		f, err := scan(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, f)
	}
	rows.Close()
	for _, f := range list {
		if _, err := w.S.scanFolder(ctx, f); err != nil {
			w.S.log.Warn("folder scan failed", "folder", f.Path, "err", err)
		}
	}
	return nil
}

var ignoredSuffix = []string{"~", ".tmp", ".part", ".partial", ".crdownload", ".download", ".swp", ".lock"}

func ignored(name string) bool {
	l := strings.ToLower(name)
	if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "~$") || l == "thumbs.db" || l == "desktop.ini" || strings.HasSuffix(l, ".error.txt") {
		return true
	}
	for _, suf := range ignoredSuffix {
		if strings.HasSuffix(l, suf) {
			return true
		}
	}
	return false
}

func (s *Service) scanFolder(ctx context.Context, f *Folder) (*ScanResult, error) {
	res := &ScanResult{}
	setErr := func(msg string) {
		_, _ = s.pool.Exec(context.Background(), `UPDATE watched_folders SET last_scan_at=now(), last_error=$2 WHERE id=$1`, f.ID, msg)
	}
	if _, err := os.Stat(f.Path); err != nil {
		setErr("The folder isn't available: " + err.Error())
		return res, err
	}
	var files []string
	err := filepath.WalkDir(f.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if p == f.Path {
				return nil
			}
			if n := d.Name(); n == "done" || n == "failed" || strings.HasPrefix(n, ".") || !f.Recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && !ignored(d.Name()) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		setErr(err.Error())
		return res, err
	}
	owner := s.owner(ctx, f.ID)
	now := time.Now()
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		s.mu.Lock()
		prev, seenBefore := s.seen[p]
		s.seen[p] = observed{size: st.Size(), mod: st.ModTime()}
		s.mu.Unlock()
		quiet := now.Sub(st.ModTime()) >= time.Duration(f.StableSeconds)*time.Second
		same := seenBefore && prev.size == st.Size() && prev.mod.Equal(st.ModTime())
		if !quiet || (f.StableSeconds > 0 && !same) || st.Size() == 0 {
			res.Skipped++
			continue
		}
		switch err := s.importFile(ctx, f, owner, p); {
		case err == nil:
			res.Imported++
		default:
			res.Failed++
			s.log.Warn("folder import failed", "file", p, "err", err)
		}
		s.mu.Lock()
		delete(s.seen, p)
		s.mu.Unlock()
	}
	_, _ = s.pool.Exec(ctx, `UPDATE watched_folders SET last_scan_at=now(), last_error='', imported_count=imported_count+$2, failed_count=failed_count+$3 WHERE id=$1`,
		f.ID, res.Imported, res.Failed)
	return res, nil
}

// target works out the space and extra tags for a file from its sub-folder.
func (s *Service) target(ctx context.Context, f *Folder, owner *auth.Principal, path string) (uuid.UUID, []uuid.UUID, error) {
	space, tags := f.SpaceID, slices.Clone(f.TagIDs)
	rel, err := filepath.Rel(f.Path, filepath.Dir(path))
	if err != nil || rel == "." {
		return space, tags, nil
	}
	sub := strings.Split(filepath.ToSlash(rel), "/")[0]
	switch f.Subfolders {
	case "tag":
		id, err := taxonomy.EnsureByName(ctx, s.pool, taxonomy.Tags, space, sub)
		if err != nil {
			return space, tags, err
		}
		tags = append(tags, id)
	case "space":
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT s.id FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$2
			WHERE lower(s.name)=lower($1) AND m.role IN ('owner','editor') LIMIT 1`, sub, owner.UserID).Scan(&id); err == nil {
			space = id
			tags = nil // tags belong to the folder's own space
		}
	}
	return space, tags, nil
}

func (s *Service) importFile(ctx context.Context, f *Folder, owner *auth.Principal, path string) error {
	space, tags, err := s.target(ctx, f, owner, path)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	_, ierr := s.docs.Ingest(ctx, owner, documents.IngestInput{SpaceID: space, Filename: filepath.Base(path), TagIDs: tags,
		Source: "folder", Priority: jobs.PriorityBulk}, file)
	file.Close()
	// A file that is already in the space is as good as imported.
	if ae, ok := apperr.As(ierr); ok && ae.Code == "duplicate_document" {
		ierr = nil
	}
	if ierr != nil {
		return s.reject(f, owner, path, ierr)
	}
	return s.finish(f, path)
}

func (s *Service) finish(f *Folder, path string) error {
	if f.AfterImport == "delete" {
		return os.Remove(path)
	}
	return moveInto(f.Path, "done", path)
}

// reject moves an unimportable file to failed/ with a note that says why.
func (s *Service) reject(f *Folder, owner *auth.Principal, path string, cause error) error {
	msg := cause.Error()
	if ae, ok := apperr.As(cause); ok && ae.Kind != apperr.KindInternal {
		msg = ae.Msg
	}
	dst, err := moveIntoPath(f.Path, "failed", path)
	if err == nil {
		_ = os.WriteFile(dst+".error.txt", []byte(msg+"\n"), 0o640)
	}
	if owner != nil && owner.UserID != uuid.Nil {
		_ = s.queue.Emit(context.Background(), jobs.Event{Type: "import.failed", Title: "Couldn't import " + filepath.Base(path), Body: msg,
			Severity: "warning", Recipients: []uuid.UUID{owner.UserID}, Link: "/admin/folders"})
	}
	return fmt.Errorf("%s: %w", filepath.Base(path), cause)
}

func moveInto(root, dir, path string) error {
	_, err := moveIntoPath(root, dir, path)
	return err
}

// moveIntoPath moves path to <root>/<dir>/<timestamp>-<name> and returns the new path.
func moveIntoPath(root, dir, path string) (string, error) {
	dstDir := filepath.Join(root, dir)
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, time.Now().Format("20060102-150405")+"-"+filepath.Base(path))
	for i := 1; ; i++ {
		if _, err := os.Stat(dst); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(dstDir, fmt.Sprintf("%s-%d-%s", time.Now().Format("20060102-150405"), i, filepath.Base(path)))
	}
	return dst, os.Rename(path, dst)
}

// owner is who imported documents belong to: the administrator who set the folder up
// (while they are still active), otherwise nobody in particular.
func (s *Service) owner(ctx context.Context, folderID uuid.UUID) *auth.Principal {
	var uid uuid.UUID
	var name, email string
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.display_name, u.email FROM watched_folders w JOIN users u ON u.id=w.created_by
		WHERE w.id=$1 AND u.status='active'`, folderID).Scan(&uid, &name, &email)
	if err != nil {
		return auth.System()
	}
	return &auth.Principal{Kind: auth.KindSession, UserID: uid, Name: name, Email: email}
}
