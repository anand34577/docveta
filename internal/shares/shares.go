// Package shares implements share links: a secret URL that lets someone without an
// account view one document, or a saved view, optionally behind a password, until it
// expires or is revoked.
package shares

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/platform/ratelimit"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/spaces"
)

const tokenPrefix = "dvt_shr"

// Share is a share link as the creator sees it. The secret URL is only shown at creation.
type Share struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"` // document | view
	DocumentID    *uuid.UUID `json:"document_id"`
	ViewID        *uuid.UUID `json:"view_id"`
	Title         string     `json:"title"`
	AllowDownload bool       `json:"allow_download"`
	HasPassword   bool       `json:"has_password"`
	Note          string     `json:"note"`
	ExpiresAt     *time.Time `json:"expires_at"`
	AccessCount   int        `json:"access_count"`
	LastAccessAt  *time.Time `json:"last_access_at"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	Status        string     `json:"status"` // active | expired | revoked
}

type NewShare struct {
	DocumentID    *uuid.UUID `json:"document_id"`
	ViewID        *uuid.UUID `json:"view_id"`
	ExpiresInDays *int       `json:"expires_in_days"` // nil = never
	Password      *string    `json:"password"`
	AllowDownload bool       `json:"allow_download"`
	Note          string     `json:"note"`
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
	docs   *documents.Service
	search *search.Service
	keys   *crypto.Keys
	audit  *audit.Log

	ipLimiter   *ratelimit.Limiter // every public request, per IP
	passLimiter *ratelimit.Limiter // wrong passwords, per share
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service, docs *documents.Service, se *search.Service, keys *crypto.Keys, al *audit.Log) *Service {
	return &Service{pool: pool, spaces: sp, docs: docs, search: se, keys: keys, audit: al,
		ipLimiter: ratelimit.New(300, 15*time.Minute), passLimiter: ratelimit.New(10, 15*time.Minute)}
}

const selectSQL = `SELECT s.id, CASE WHEN s.document_id IS NOT NULL THEN 'document' ELSE 'view' END, s.document_id, s.view_id,
	coalesce(d.title, v.name, ''), s.allow_download, s.password_hash IS NOT NULL, s.note, s.expires_at, s.access_count, s.last_access_at,
	coalesce(u.display_name, ''), s.created_at,
	CASE WHEN s.revoked_at IS NOT NULL THEN 'revoked' WHEN s.expires_at IS NOT NULL AND s.expires_at < now() THEN 'expired' ELSE 'active' END
	FROM shares s LEFT JOIN documents d ON d.id=s.document_id LEFT JOIN saved_views v ON v.id=s.view_id LEFT JOIN users u ON u.id=s.created_by`

func scan(row pgx.Row) (*Share, error) {
	var s Share
	err := row.Scan(&s.ID, &s.Kind, &s.DocumentID, &s.ViewID, &s.Title, &s.AllowDownload, &s.HasPassword, &s.Note, &s.ExpiresAt,
		&s.AccessCount, &s.LastAccessAt, &s.CreatedBy, &s.CreatedAt, &s.Status)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Share link")
	}
	return &s, err
}

// Create makes a share link for a document or a saved view. Sharing outside the
// organisation needs edit rights in the space.
func (s *Service) Create(ctx context.Context, p *auth.Principal, in NewShare) (*Share, string, error) {
	if (in.DocumentID == nil) == (in.ViewID == nil) {
		return nil, "", apperr.Invalid("document_id", "Share either a document or a saved view")
	}
	var v apperr.Validation
	if in.ExpiresInDays != nil && (*in.ExpiresInDays < 1 || *in.ExpiresInDays > 3650) {
		v.Add("expires_in_days", "Must be between 1 and 3650 days")
	}
	if in.Password != nil {
		if n := utf8.RuneCountInString(*in.Password); n == 0 {
			in.Password = nil
		} else if n < 6 || n > 128 {
			v.Add("password", "Use 6–128 characters")
		}
	}
	if utf8.RuneCountInString(in.Note) > 500 {
		v.Add("note", "Too long")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	var spaceID uuid.UUID
	if in.DocumentID != nil {
		a, err := s.docs.Access(ctx, p, *in.DocumentID, spaces.ActEdit)
		if err != nil {
			return nil, "", err
		}
		if a.Deleted {
			return nil, "", apperr.Conflict("in_trash", "Restore this document before sharing it")
		}
		spaceID = a.SpaceID
	} else {
		var sp *uuid.UUID
		var owner uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT space_id, owner_id FROM saved_views WHERE id=$1`, *in.ViewID).Scan(&sp, &owner); err != nil {
			return nil, "", apperr.NotFound("Saved view")
		}
		if sp == nil {
			return nil, "", apperr.Invalid("view_id", "Only a view that belongs to a space can be shared. Save it for a space first.")
		}
		if _, err := s.spaces.Require(ctx, p, *sp, spaces.ActEdit); err != nil {
			return nil, "", err
		}
		spaceID = *sp
	}
	var hash *string
	if in.Password != nil {
		h, err := crypto.HashPassword(*in.Password)
		if err != nil {
			return nil, "", err
		}
		hash = &h
	}
	var exp *time.Time
	if in.ExpiresInDays != nil {
		t := time.Now().Add(time.Duration(*in.ExpiresInDays) * 24 * time.Hour)
		exp = &t
	}
	token, _ := crypto.NewToken(tokenPrefix)
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO shares (id, token_hash, document_id, view_id, space_id, created_by, password_hash, allow_download, note, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, crypto.HashToken(token), in.DocumentID, in.ViewID, spaceID, p.UserID, hash, in.AllowDownload, in.Note, exp); err != nil {
		return nil, "", err
	}
	s.audit.Record(ctx, nil, "share.create", "share", id.String(), map[string]any{"document_id": in.DocumentID, "view_id": in.ViewID,
		"password": hash != nil, "download": in.AllowDownload, "expires": exp})
	sh, err := scan(s.pool.QueryRow(ctx, selectSQL+` WHERE s.id=$1`, id))
	return sh, token, err
}

// List returns share links for a document or view that the caller created, or that
// belong to spaces they own.
func (s *Service) List(ctx context.Context, p *auth.Principal, documentID, viewID *uuid.UUID) ([]*Share, error) {
	rows, err := s.pool.Query(ctx, selectSQL+` LEFT JOIN space_members m ON m.space_id=s.space_id AND m.user_id=$1
		WHERE ($2::uuid IS NULL OR s.document_id=$2) AND ($3::uuid IS NULL OR s.view_id=$3)
		  AND (s.created_by=$1 OR m.role='owner') ORDER BY s.created_at DESC LIMIT 200`, p.UserID, documentID, viewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Share{}
	for rows.Next() {
		sh, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Service) Revoke(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shares SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL AND (created_by=$2
		OR EXISTS (SELECT 1 FROM space_members m WHERE m.space_id=shares.space_id AND m.user_id=$2 AND m.role='owner'))`, id, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Share link")
	}
	s.audit.Record(ctx, nil, "share.revoke", "share", id.String(), nil)
	return nil
}

// ---------------------------------------------------------------------------
// Public side
// ---------------------------------------------------------------------------

var errGone = &apperr.Error{Kind: apperr.KindNotFound, Code: "share_unavailable",
	Msg: "This link isn't available any more. It may have expired or been turned off."}

type resolved struct {
	ID            uuid.UUID
	Kind          string
	DocumentID    *uuid.UUID
	ViewID        *uuid.UUID
	CreatedBy     uuid.UUID
	SpaceID       uuid.UUID
	PasswordHash  *string
	AllowDownload bool
	ExpiresAt     *time.Time
	Title         string
}

func (s *Service) limited(ctx context.Context) error {
	if ok, wait := s.ipLimiter.Allow(httpx.ClientIP(ctx)); !ok {
		return &apperr.Error{Kind: apperr.KindRateLimited, Code: "rate_limited", Msg: "Too many requests. Please wait a few minutes.",
			Extra: map[string]any{"retry_after_seconds": int(wait.Seconds()) + 1}}
	}
	return nil
}

func (s *Service) resolve(ctx context.Context, token string) (*resolved, error) {
	if err := s.limited(ctx); err != nil {
		return nil, err
	}
	var r resolved
	var created *uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.document_id, s.view_id, s.created_by, s.space_id, s.password_hash, s.allow_download, s.expires_at,
		coalesce(d.title, v.name, ''), (s.document_id IS NOT NULL AND d.deleted_at IS NOT NULL)
		FROM shares s LEFT JOIN documents d ON d.id=s.document_id LEFT JOIN saved_views v ON v.id=s.view_id
		WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > now())`, crypto.HashToken(token)).
		Scan(&r.ID, &r.DocumentID, &r.ViewID, &created, &r.SpaceID, &r.PasswordHash, &r.AllowDownload, &r.ExpiresAt, &r.Title, new(bool))
	if db.IsNoRows(err) {
		return nil, errGone
	}
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, errGone // a share needs a person whose access it borrows
	}
	r.CreatedBy = *created
	r.Kind = "document"
	if r.ViewID != nil {
		r.Kind = "view"
	}
	return &r, nil
}

// UnlockCookie is the cookie name/value pair that proves the password was entered.
func (s *Service) cookieName(id uuid.UUID) string { return "dvt_share_" + id.String()[:8] }

func (s *Service) CookieName(ctx context.Context, token string) (string, error) {
	r, err := s.resolve(ctx, token)
	if err != nil {
		return "", err
	}
	return s.cookieName(r.ID), nil
}

func (s *Service) unlocked(r *resolved, cookie string) bool {
	return r.PasswordHash == nil || s.keys.VerifySignedValue("share-unlock", r.ID.String(), cookie, time.Now())
}

// Unlock checks the password and returns the value for the unlock cookie.
func (s *Service) Unlock(ctx context.Context, token, password string) (name, value string, ttl time.Duration, err error) {
	r, err := s.resolve(ctx, token)
	if err != nil {
		return "", "", 0, err
	}
	if r.PasswordHash == nil {
		return s.cookieName(r.ID), "", 0, nil
	}
	if ok, wait := s.passLimiter.Allow(r.ID.String()); !ok {
		return "", "", 0, &apperr.Error{Kind: apperr.KindRateLimited, Code: "rate_limited", Msg: "Too many wrong passwords. Please wait a few minutes.",
			Extra: map[string]any{"retry_after_seconds": int(wait.Seconds()) + 1}}
	}
	if ok, err := crypto.VerifyPassword(password, *r.PasswordHash); err != nil || !ok {
		return "", "", 0, apperr.Invalid("password", "That password isn't right")
	}
	ttl = 6 * time.Hour
	return s.cookieName(r.ID), s.keys.SignedValue("share-unlock", r.ID.String(), time.Now().Add(ttl)), ttl, nil
}

// PublicDoc is all a visitor learns about a document.
type PublicDoc struct {
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	DocumentDate *string   `json:"document_date"`
	MimeType     string    `json:"mime_type"`
	PageCount    *int      `json:"page_count"`
	SizeBytes    int64     `json:"size_bytes"`
	HasArchive   bool      `json:"has_archive"`
	HasThumbnail bool      `json:"has_thumbnail"`
	HasDerived   bool      `json:"has_derived"`
}

type Public struct {
	Kind             string      `json:"kind"`
	Title            string      `json:"title"`
	RequiresPassword bool        `json:"requires_password"`
	AllowDownload    bool        `json:"allow_download"`
	ExpiresAt        *time.Time  `json:"expires_at"`
	Documents        []PublicDoc `json:"documents"`
}

func pub(d *documents.Document) PublicDoc {
	return PublicDoc{ID: d.ID, Title: d.Title, DocumentDate: d.DocumentDate, MimeType: d.MimeType, PageCount: d.PageCount,
		SizeBytes: d.SizeBytes, HasArchive: d.HasArchive, HasThumbnail: d.HasThumbnail, HasDerived: d.HasDerived}
}

// docs lists the documents a share exposes. A view share runs the saved search as its
// creator, so it never shows more than they can see right now.
func (s *Service) docs_(ctx context.Context, r *resolved) ([]*documents.Document, error) {
	creator := &auth.Principal{Kind: auth.KindSession, UserID: r.CreatedBy}
	if r.DocumentID != nil {
		if _, err := s.docs.Access(ctx, creator, *r.DocumentID, spaces.ActView); err != nil {
			return nil, errGone
		}
		d, err := s.docs.Hydrated(ctx, *r.DocumentID)
		if err != nil || d.DeletedAt != nil {
			return nil, errGone
		}
		return []*documents.Document{d}, nil
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT query FROM saved_views WHERE id=$1`, *r.ViewID).Scan(&raw); err != nil {
		return nil, errGone
	}
	var q search.Query
	_ = json.Unmarshal(raw, &q)
	q.SpaceIDs = []uuid.UUID{r.SpaceID} // a share never reaches beyond its own space
	q.Limit, q.Cursor, q.Trash = 200, "", false
	res, err := s.docs.List(ctx, creator, q)
	if err != nil {
		return nil, errGone
	}
	return res.Items, nil
}

// Meta returns what the visitor may see. Without the password only the title and the
// fact that a password is needed are returned.
func (s *Service) Meta(ctx context.Context, token, cookie string) (*Public, error) {
	r, err := s.resolve(ctx, token)
	if err != nil {
		return nil, err
	}
	out := &Public{Kind: r.Kind, Title: r.Title, RequiresPassword: r.PasswordHash != nil && !s.unlocked(r, cookie),
		AllowDownload: r.AllowDownload, ExpiresAt: r.ExpiresAt, Documents: []PublicDoc{}}
	if out.RequiresPassword {
		return out, nil
	}
	list, err := s.docs_(ctx, r)
	if err != nil {
		return nil, err
	}
	for _, d := range list {
		out.Documents = append(out.Documents, pub(d))
	}
	_, _ = s.pool.Exec(ctx, `UPDATE shares SET access_count=access_count+1, last_access_at=now() WHERE id=$1`, r.ID)
	return out, nil
}

// File opens a file of a shared document for the visitor.
func (s *Service) File(ctx context.Context, token, cookie string, docID uuid.UUID, kind string, download bool) (*documents.File, error) {
	r, err := s.resolve(ctx, token)
	if err != nil {
		return nil, err
	}
	if !s.unlocked(r, cookie) {
		return nil, &apperr.Error{Kind: apperr.KindUnauthorized, Code: "password_required", Msg: "Enter the password for this link"}
	}
	if download && !r.AllowDownload {
		return nil, apperr.Forbidden("Downloads are turned off for this link")
	}
	if kind == "original" && !r.AllowDownload {
		kind = "best"
	}
	allowed := r.DocumentID != nil && *r.DocumentID == docID
	if !allowed {
		list, err := s.docs_(ctx, r)
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			allowed = allowed || d.ID == docID
		}
	}
	if !allowed {
		return nil, apperr.NotFound("Document")
	}
	sys := auth.System()
	if kind == "best" {
		if f, err := s.docs.OpenFile(ctx, sys, docID, "archive"); err == nil {
			return f, nil
		}
		kind = "original"
	}
	switch kind {
	case "original", "archive", "thumbnail", "derived":
	default:
		return nil, apperr.Invalid("kind", "Unknown file kind")
	}
	return s.docs.OpenFile(ctx, sys, docID, kind)
}
