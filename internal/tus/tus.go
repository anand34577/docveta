// Package tus implements resumable uploads (the tus 1.0.0 protocol: creation,
// termination, expiration) so large scans and phone photos survive flaky networks: a
// client asks how much arrived (HEAD) and continues from there (PATCH). When the last
// byte lands the file becomes a document exactly like a normal upload.
package tus

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
)

const (
	Version = "1.0.0"
	ttl     = 24 * time.Hour // an unfinished upload is kept this long after its last activity
)

type Upload struct {
	ID        uuid.UUID
	Length    int64
	Offset    int64
	Metadata  map[string]string
	ExpiresAt time.Time
}

type Service struct {
	pool    *pgxpool.Pool
	docs    *documents.Service
	spaces  *spaces.Service
	dir     string
	maxSize int64

	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex // one writer per upload at a time
}

func NewService(pool *pgxpool.Pool, docs *documents.Service, sp *spaces.Service, dataDir string, maxSize int64) (*Service, error) {
	dir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Service{pool: pool, docs: docs, spaces: sp, dir: dir, maxSize: maxSize, locks: map[uuid.UUID]*sync.Mutex{}}, nil
}

func (s *Service) MaxSize() int64 { return s.maxSize }

func (s *Service) path(id uuid.UUID) string { return filepath.Join(s.dir, id.String()) }

func (s *Service) lock(id uuid.UUID) func() {
	s.mu.Lock()
	l := s.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	s.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		s.mu.Lock()
		delete(s.locks, id)
		s.mu.Unlock()
	}
}

// ParseMetadata decodes the Upload-Metadata header: "key base64value,key2 base64value2".
func ParseMetadata(h string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, hasVal := strings.Cut(part, " ")
		if k == "" || strings.ContainsAny(k, " ,") {
			return nil, errors.New("bad metadata key")
		}
		if !hasVal {
			out[k] = ""
			continue
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err != nil {
			if b, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(v)); err != nil {
				return nil, errors.New("bad metadata value")
			}
		}
		out[k] = string(b)
	}
	return out, nil
}

var errNotFound = apperr.NotFound("Upload")

// Create starts an upload of the given length. Metadata describes the document-to-be.
func (s *Service) Create(ctx context.Context, p *auth.Principal, length int64, meta map[string]string) (*Upload, error) {
	if p == nil || (!p.Has(auth.ScopeUpload) && !p.Has(auth.ScopeWrite)) {
		return nil, apperr.Forbidden("This token can't upload documents")
	}
	if length <= 0 {
		return nil, apperr.Invalid("Upload-Length", "The file is empty")
	}
	if length > s.maxSize {
		return nil, &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large", Msg: fmt.Sprintf("This file is larger than the %d MB limit", s.maxSize>>20)}
	}
	if _, err := s.spaceOf(ctx, p, meta); err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	f, err := os.OpenFile(s.path(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	raw, _ := json.Marshal(meta)
	exp := time.Now().Add(ttl)
	if _, err := s.pool.Exec(ctx, `INSERT INTO uploads (id, user_id, length, metadata, expires_at) VALUES ($1,$2,$3,$4,$5)`, id, p.UserID, length, raw, exp); err != nil {
		_ = os.Remove(s.path(id))
		return nil, err
	}
	return &Upload{ID: id, Length: length, Metadata: meta, ExpiresAt: exp}, nil
}

// spaceOf resolves the target space (default: the user's first space) and checks the
// user may add documents there, so a bad choice fails before gigabytes are sent.
func (s *Service) spaceOf(ctx context.Context, p *auth.Principal, meta map[string]string) (uuid.UUID, error) {
	if v := meta["space_id"]; v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, apperr.Invalid("space_id", "Invalid id")
		}
		_, err = s.spaces.Require(ctx, p, id, spaces.ActUpload)
		return id, err
	}
	sp, err := s.spaces.List(ctx, p)
	if err != nil || len(sp) == 0 {
		return uuid.Nil, apperr.Invalid("space_id", "Choose a space")
	}
	return sp[0].ID, nil
}

func (s *Service) load(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Upload, error) {
	var u Upload
	var raw []byte
	var owner uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id, length, "offset", metadata, expires_at, user_id FROM uploads WHERE id=$1 AND expires_at > now()`, id).
		Scan(&u.ID, &u.Length, &u.Offset, &raw, &u.ExpiresAt, &owner)
	if db.IsNoRows(err) || (err == nil && owner != p.UserID) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &u.Metadata)
	return &u, nil
}

// Head reports how much of the upload has arrived.
func (s *Service) Head(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Upload, error) {
	return s.load(ctx, p, id)
}

// ErrOffset means the client's offset doesn't match what the server has.
var ErrOffset = &apperr.Error{Kind: apperr.KindConflict, Code: "offset_mismatch", Msg: "The upload is at a different position. Ask where it is (HEAD) and continue from there."}

// Patch appends the request body at offset. When the file is complete it becomes a
// document, returned as doc. Bytes received before a dropped connection are kept.
func (s *Service) Patch(ctx context.Context, p *auth.Principal, id uuid.UUID, offset int64, body io.Reader) (newOffset int64, doc *documents.Document, err error) {
	defer s.lock(id)()
	u, err := s.load(ctx, p, id)
	if err != nil {
		return 0, nil, err
	}
	if offset != u.Offset {
		return u.Offset, nil, ErrOffset
	}
	f, err := os.OpenFile(s.path(id), os.O_WRONLY, 0o640)
	if err != nil {
		return u.Offset, nil, errNotFound
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return u.Offset, nil, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(body, u.Length-offset))
	if syncErr := f.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	_ = f.Close()
	newOffset = offset + n
	// Record progress even if the connection dropped part-way: that's the point of resuming.
	if _, err := s.pool.Exec(context.WithoutCancel(ctx), `UPDATE uploads SET "offset"=$2, expires_at=$3, updated_at=now() WHERE id=$1`, id, newOffset, time.Now().Add(ttl)); err != nil {
		return newOffset, nil, err
	}
	if copyErr != nil {
		return newOffset, nil, copyErr
	}
	if newOffset < u.Length {
		return newOffset, nil, nil
	}
	doc, err = s.finish(ctx, p, u)
	return newOffset, doc, err
}

func (s *Service) finish(ctx context.Context, p *auth.Principal, u *Upload) (*documents.Document, error) {
	defer s.discard(context.WithoutCancel(ctx), u.ID)
	space, err := s.spaceOf(ctx, p, u.Metadata)
	if err != nil {
		return nil, err
	}
	in := documents.IngestInput{SpaceID: space, Filename: u.Metadata["filename"], Title: u.Metadata["title"], Language: u.Metadata["language"],
		AllowDuplicate: u.Metadata["allow_duplicate"] == "true", Source: "web"}
	if p.Kind == auth.KindToken {
		in.Source = "api"
	}
	if src := u.Metadata["source"]; src == "share" || src == "scan" {
		in.Source = src
	}
	if d := u.Metadata["document_date"]; d != "" {
		in.DocumentDate = &d
	}
	for _, t := range strings.Split(u.Metadata["tag_ids"], ",") {
		if id, err := uuid.Parse(strings.TrimSpace(t)); err == nil {
			in.TagIDs = append(in.TagIDs, id)
		}
	}
	for k, dst := range map[string]**uuid.UUID{"correspondent_id": &in.CorrespondentID, "document_type_id": &in.DocumentTypeID} {
		if id, err := uuid.Parse(u.Metadata[k]); err == nil {
			*dst = &id
		}
	}
	f, err := os.Open(s.path(u.ID))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return s.docs.Ingest(ctx, p, in, f)
}

func (s *Service) discard(ctx context.Context, id uuid.UUID) {
	_ = os.Remove(s.path(id))
	_, _ = s.pool.Exec(ctx, `DELETE FROM uploads WHERE id=$1`, id)
}

// Delete cancels an upload and frees its space.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	defer s.lock(id)()
	if _, err := s.load(ctx, p, id); err != nil {
		return err
	}
	s.discard(ctx, id)
	return nil
}

// Cleanup removes expired uploads and stray files.
func (s *Service) Cleanup(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `DELETE FROM uploads WHERE expires_at < now() RETURNING id`)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				_ = os.Remove(s.path(id))
			}
		}
		rows.Close()
	}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		id, err := uuid.Parse(e.Name())
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < 2*ttl {
			continue
		}
		var known bool
		if s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM uploads WHERE id=$1)`, id).Scan(&known) == nil && !known {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}
