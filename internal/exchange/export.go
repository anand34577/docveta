// Package exchange exports a whole Docveta (users, spaces, vocabulary, documents with
// their text, notes and files) to a folder or zip with readable names and a versioned
// manifest, and imports such an export again. Importing is idempotent: running it twice
// changes nothing the second time.
//
// Layout:
//
//	manifest.json     users, spaces, tags, correspondents, types, custom fields, saved views
//	documents.jsonl   one document per line: metadata, text per page, notes, custom fields
//	files/<space>/<year>/<title>-<id>.<ext>   originals; also .searchable.pdf and .thumb.jpg
package exchange

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/storage"
)

const Schema = "docveta-export/v1"

// ---------------------------------------------------------------------------
// File format
// ---------------------------------------------------------------------------

type Manifest struct {
	Schema         string         `json:"schema"`
	ExportedAt     time.Time      `json:"exported_at"`
	Version        string         `json:"docveta_version"`
	Users          []UserRec      `json:"users"`
	Spaces         []SpaceRec     `json:"spaces"`
	Tags           []VocabRec     `json:"tags"`
	Correspondents []VocabRec     `json:"correspondents"`
	DocumentTypes  []VocabRec     `json:"document_types"`
	CustomFields   []FieldRec     `json:"custom_fields"`
	SavedViews     []ViewRec      `json:"saved_views"`
	Counts         map[string]int `json:"counts"`
}

type UserRec struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	IsAdmin      bool      `json:"is_admin"`
	Status       string    `json:"status"`
	Locale       string    `json:"locale"`
	Timezone     string    `json:"timezone"`
	DateFormat   string    `json:"date_format"`
	PasswordHash *string   `json:"password_hash,omitempty"` // only with --with-password-hashes
}

type SpaceRec struct {
	ID              uuid.UUID   `json:"id"`
	Name            string      `json:"name"`
	Kind            string      `json:"kind"`
	Description     string      `json:"description"`
	Color           string      `json:"color"`
	DefaultLanguage string      `json:"default_language"`
	AIPolicy        string      `json:"ai_policy"`
	AIApplyMode     string      `json:"ai_apply_mode"`
	Members         []MemberRec `json:"members"`
}

type MemberRec struct {
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
}

type VocabRec struct {
	ID             uuid.UUID `json:"id"`
	SpaceID        uuid.UUID `json:"space_id"`
	Name           string    `json:"name"`
	Color          string    `json:"color,omitempty"`
	MatchAlgorithm string    `json:"match_algorithm"`
	MatchPattern   string    `json:"match_pattern"`
	CaseSensitive  bool      `json:"case_sensitive"`
}

type FieldRec struct {
	ID       uuid.UUID       `json:"id"`
	SpaceID  uuid.UUID       `json:"space_id"`
	Name     string          `json:"name"`
	DataType string          `json:"data_type"`
	Options  json.RawMessage `json:"options"`
}

type ViewRec struct {
	ID      uuid.UUID       `json:"id"`
	SpaceID *uuid.UUID      `json:"space_id"`
	OwnerID uuid.UUID       `json:"owner_id"`
	Name    string          `json:"name"`
	Query   json.RawMessage `json:"query"`
	Pinned  bool            `json:"pinned"`
}

type DocRec struct {
	ID               uuid.UUID        `json:"id"`
	SpaceID          uuid.UUID        `json:"space_id"`
	OwnerID          *uuid.UUID       `json:"owner_id"`
	Title            string           `json:"title"`
	DocumentDate     *string          `json:"document_date"`
	AddedAt          time.Time        `json:"added_at"`
	CorrespondentID  *uuid.UUID       `json:"correspondent_id"`
	DocumentTypeID   *uuid.UUID       `json:"document_type_id"`
	TagIDs           []uuid.UUID      `json:"tag_ids"`
	Language         string           `json:"language"`
	PageCount        *int             `json:"page_count"`
	ASN              *int64           `json:"asn"`
	PhysicalLocation string           `json:"physical_location"`
	Inbox            bool             `json:"inbox"`
	Status           string           `json:"status"`
	ProcessingError  string           `json:"processing_error,omitempty"`
	MimeType         string           `json:"mime_type"`
	OriginalFilename string           `json:"original_filename"`
	Source           string           `json:"source"`
	SHA256           string           `json:"sha256"`
	Original         string           `json:"original"`            // path inside the export
	Archive          string           `json:"archive,omitempty"`   // searchable PDF
	Thumbnail        string           `json:"thumbnail,omitempty"` // JPEG
	Pages            []PageRec        `json:"pages"`
	Notes            []NoteRec        `json:"notes"`
	CustomFields     []CustomValueRec `json:"custom_fields"`
}

type PageRec struct {
	No         int      `json:"page"`
	Text       string   `json:"text"`
	Confidence *float32 `json:"confidence,omitempty"`
	Rotation   int      `json:"rotation,omitempty"`
}

type NoteRec struct {
	AuthorID  *uuid.UUID `json:"author_id"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
}

type CustomValueRec struct {
	FieldID uuid.UUID `json:"field_id"`
	Value   any       `json:"value"`
}

// ---------------------------------------------------------------------------
// Sinks
// ---------------------------------------------------------------------------

// Sink receives the exported files.
type Sink interface {
	Put(name string, r io.Reader) error
	Close() error
}

type dirSink struct{ root string }

// DirSink writes an export into a folder.
func DirSink(root string) Sink { return dirSink{root} }

func (d dirSink) Put(name string, r io.Reader) error {
	p := filepath.Join(d.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (dirSink) Close() error { return nil }

type zipSink struct{ zw *zip.Writer }

// ZipSink streams an export as a zip file.
func ZipSink(w io.Writer) Sink { return zipSink{zip.NewWriter(w)} }

func (z zipSink) Put(name string, r io.Reader) error {
	// Originals are already compressed (PDF, JPEG): store them; compress the JSON.
	method := zip.Deflate
	if strings.HasPrefix(name, "files/") {
		method = zip.Store
	}
	w, err := z.zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

func (z zipSink) Close() error { return z.zw.Close() }

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

type ExportOptions struct {
	Version        string
	PasswordHashes bool // include password hashes (a full instance move); off by default
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func safeName(s string, max int) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, " "))
	s = strings.Trim(strings.Join(strings.Fields(s), " "), ". ")
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max]))
	}
	if s == "" {
		return "untitled"
	}
	return s
}

// Export writes everything to the sink. Trashed documents are left out.
func Export(ctx context.Context, pool *pgxpool.Pool, store storage.Store, sink Sink, opt ExportOptions) (map[string]int, error) {
	m := &Manifest{Schema: Schema, ExportedAt: time.Now().UTC(), Version: opt.Version, Counts: map[string]int{}}
	if err := loadManifest(ctx, pool, m, opt); err != nil {
		return nil, err
	}
	spaceName := map[uuid.UUID]string{}
	for _, s := range m.Spaces {
		n := s.Name
		if s.Kind == "personal" {
			n = "Personal - " + s.Name
		}
		spaceName[s.ID] = safeName(n, 60)
	}

	tmp, err := os.CreateTemp("", "docveta-export-*.jsonl")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	enc := bufio.NewWriter(tmp)

	rows, err := pool.Query(ctx, `SELECT id FROM documents WHERE deleted_at IS NULL ORDER BY added_at, id`)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		rec, err := loadDoc(ctx, pool, id)
		if err != nil {
			return nil, err
		}
		base := path.Join("files", spaceName[rec.SpaceID], fmt.Sprint(rec.AddedAt.Year()), safeName(rec.Title, 80)+" - "+id.String()[:8])
		put := func(kind, dst string) (string, error) {
			var key string
			if err := pool.QueryRow(ctx, `SELECT f.blob_key FROM document_files f JOIN documents d ON d.id=f.document_id
				WHERE f.document_id=$1 AND f.kind=$2 AND f.version_no=d.current_version`, id, kind).Scan(&key); err != nil {
				return "", nil // no such file: fine
			}
			r, _, err := store.Open(ctx, key)
			if err != nil {
				return "", fmt.Errorf("document %s: %s is missing from storage: %w", id, kind, err)
			}
			defer r.Close()
			return dst, sink.Put(dst, r)
		}
		if rec.Original, err = put("original", base+documents.ExtFor(rec.MimeType)); err != nil {
			return nil, err
		}
		if rec.Original == "" {
			continue // nothing to export without the original
		}
		if rec.Archive, err = put("archive", base+".searchable.pdf"); err != nil {
			return nil, err
		}
		if rec.Thumbnail, err = put("thumbnail", base+".thumb.jpg"); err != nil {
			return nil, err
		}
		line, _ := json.Marshal(rec)
		enc.Write(line)
		enc.WriteByte('\n')
		m.Counts["documents"]++
		m.Counts["notes"] += len(rec.Notes)
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	m.Counts["users"], m.Counts["spaces"] = len(m.Users), len(m.Spaces)
	mj, _ := json.MarshalIndent(m, "", "  ")
	if err := sink.Put("manifest.json", strings.NewReader(string(mj))); err != nil {
		return nil, err
	}
	if err := sink.Put("documents.jsonl", tmp); err != nil {
		return nil, err
	}
	return m.Counts, sink.Close()
}

func collect[T any](rows pgx.Rows, scan func(pgx.Rows) (T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func loadManifest(ctx context.Context, pool *pgxpool.Pool, m *Manifest, opt ExportOptions) error {
	var err error
	if m.Users, err = collect(mustQuery(pool.Query(ctx, `SELECT id, email, display_name, is_admin, status, locale, timezone, date_format,
		CASE WHEN $1 THEN password_hash END FROM users ORDER BY created_at`, opt.PasswordHashes)), func(r pgx.Rows) (UserRec, error) {
		var u UserRec
		err := r.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.Status, &u.Locale, &u.Timezone, &u.DateFormat, &u.PasswordHash)
		return u, err
	}); err != nil {
		return err
	}
	if m.Spaces, err = collect(mustQuery(pool.Query(ctx, `SELECT id, name, kind, description, color, default_language, ai_policy, ai_apply_mode FROM spaces ORDER BY created_at`)),
		func(r pgx.Rows) (SpaceRec, error) {
			var s SpaceRec
			err := r.Scan(&s.ID, &s.Name, &s.Kind, &s.Description, &s.Color, &s.DefaultLanguage, &s.AIPolicy, &s.AIApplyMode)
			s.Members = []MemberRec{}
			return s, err
		}); err != nil {
		return err
	}
	for i := range m.Spaces {
		if m.Spaces[i].Members, err = collect(mustQuery(pool.Query(ctx, `SELECT user_id, role FROM space_members WHERE space_id=$1`, m.Spaces[i].ID)),
			func(r pgx.Rows) (MemberRec, error) {
				var x MemberRec
				return x, r.Scan(&x.UserID, &x.Role)
			}); err != nil {
			return err
		}
	}
	vocab := func(table string, color string) ([]VocabRec, error) {
		return collect(mustQuery(pool.Query(ctx, `SELECT id, space_id, name, `+color+`, match_algorithm, match_pattern, case_sensitive FROM `+table+` ORDER BY lower(name)`)),
			func(r pgx.Rows) (VocabRec, error) {
				var v VocabRec
				return v, r.Scan(&v.ID, &v.SpaceID, &v.Name, &v.Color, &v.MatchAlgorithm, &v.MatchPattern, &v.CaseSensitive)
			})
	}
	if m.Tags, err = vocab("tags", "color"); err != nil {
		return err
	}
	if m.Correspondents, err = vocab("correspondents", "''"); err != nil {
		return err
	}
	if m.DocumentTypes, err = vocab("document_types", "''"); err != nil {
		return err
	}
	if m.CustomFields, err = collect(mustQuery(pool.Query(ctx, `SELECT id, space_id, name, data_type, options FROM custom_fields ORDER BY lower(name)`)),
		func(r pgx.Rows) (FieldRec, error) {
			var f FieldRec
			return f, r.Scan(&f.ID, &f.SpaceID, &f.Name, &f.DataType, &f.Options)
		}); err != nil {
		return err
	}
	m.SavedViews, err = collect(mustQuery(pool.Query(ctx, `SELECT id, space_id, owner_id, name, query, pinned FROM saved_views ORDER BY sort_order`)),
		func(r pgx.Rows) (ViewRec, error) {
			var v ViewRec
			return v, r.Scan(&v.ID, &v.SpaceID, &v.OwnerID, &v.Name, &v.Query, &v.Pinned)
		})
	return err
}

// mustQuery lets collect take pool.Query's two results directly; a query error surfaces
// when rows are read.
func mustQuery(rows pgx.Rows, err error) pgx.Rows {
	if err != nil {
		return errRows{err: err}
	}
	return rows
}

type errRows struct {
	pgx.Rows
	err error
}

func (e errRows) Next() bool { return false }
func (e errRows) Close()     {}
func (e errRows) Err() error { return e.err }

func loadDoc(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*DocRec, error) {
	var r DocRec
	var sum []byte
	err := pool.QueryRow(ctx, `SELECT id, space_id, owner_id, title, to_char(document_date,'YYYY-MM-DD'), added_at, correspondent_id, document_type_id,
		language, page_count, asn, physical_location, inbox, status, processing_error, mime_type, original_filename, source, content_hash
		FROM documents WHERE id=$1`, id).Scan(&r.ID, &r.SpaceID, &r.OwnerID, &r.Title, &r.DocumentDate, &r.AddedAt, &r.CorrespondentID, &r.DocumentTypeID,
		&r.Language, &r.PageCount, &r.ASN, &r.PhysicalLocation, &r.Inbox, &r.Status, &r.ProcessingError, &r.MimeType, &r.OriginalFilename, &r.Source, &sum)
	if err != nil {
		return nil, err
	}
	r.SHA256 = fmt.Sprintf("%x", sum)
	if r.TagIDs, err = collect(mustQuery(pool.Query(ctx, `SELECT tag_id FROM document_tags WHERE document_id=$1`, id)),
		func(rows pgx.Rows) (uuid.UUID, error) {
			var t uuid.UUID
			return t, rows.Scan(&t)
		}); err != nil {
		return nil, err
	}
	if r.Pages, err = collect(mustQuery(pool.Query(ctx, `SELECT page_no, text, confidence, rotation FROM pages WHERE document_id=$1 ORDER BY page_no`, id)),
		func(rows pgx.Rows) (PageRec, error) {
			var p PageRec
			return p, rows.Scan(&p.No, &p.Text, &p.Confidence, &p.Rotation)
		}); err != nil {
		return nil, err
	}
	if r.Notes, err = collect(mustQuery(pool.Query(ctx, `SELECT author_id, body, created_at FROM notes WHERE document_id=$1 ORDER BY created_at`, id)),
		func(rows pgx.Rows) (NoteRec, error) {
			var n NoteRec
			return n, rows.Scan(&n.AuthorID, &n.Body, &n.CreatedAt)
		}); err != nil {
		return nil, err
	}
	vals, err := customfields.Load(ctx, pool, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	r.CustomFields = []CustomValueRec{}
	for _, v := range vals[id] {
		r.CustomFields = append(r.CustomFields, CustomValueRec{FieldID: v.FieldID, Value: v.Value})
	}
	return &r, nil
}
