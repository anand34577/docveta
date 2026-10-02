package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/opt"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/storage"
	"github.com/anand34577/docveta/internal/taxonomy"
)

type Service struct {
	pool      *pgxpool.Pool
	store     storage.Store
	spaces    *spaces.Service
	search    *search.Service
	queue     *jobs.Queue
	log       *slog.Logger
	maxUpload int64
	// OfficeEnabled reports whether office conversion (Gotenberg) is configured.
	OfficeEnabled func() bool
}

func NewService(pool *pgxpool.Pool, store storage.Store, sp *spaces.Service, se *search.Service, q *jobs.Queue, log *slog.Logger, maxUpload int64) *Service {
	return &Service{pool: pool, store: store, spaces: sp, search: se, queue: q, log: log, maxUpload: maxUpload,
		OfficeEnabled: func() bool { return false }}
}

// minFreeBytes is the free space we always keep so the database and OS don't starve.
const minFreeBytes = 200 << 20

// ---------------------------------------------------------------------------
// Ingest
// ---------------------------------------------------------------------------

type IngestInput struct {
	SpaceID         uuid.UUID
	Filename        string
	Title           string
	TagIDs          []uuid.UUID
	CorrespondentID *uuid.UUID
	DocumentTypeID  *uuid.UUID
	DocumentDate    *string
	Language        string
	Source          string // web | api | share | email | folder | import
	AllowDuplicate  bool
	Priority        int
}

var titleCleaner = regexp.MustCompile(`[_\s]+`)

// TitleFromFilename turns "scan_2026-08-05 BESCOM_bill.pdf" into "scan 2026-08-05 BESCOM bill".
func TitleFromFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.TrimSpace(titleCleaner.ReplaceAllString(name, " "))
	if name == "" || name == "." {
		return "Untitled document"
	}
	return truncate(name, 300)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// Ingest stores a new document and schedules processing. The blob is durable before
// the database row is committed, and the processing job is enqueued in the same
// transaction as the row (DESIGN §10.2, NFR-5).
func (s *Service) Ingest(ctx context.Context, p *auth.Principal, in IngestInput, r io.Reader) (*Document, error) {
	if p == nil || (!p.Has(auth.ScopeUpload) && !p.Has(auth.ScopeWrite)) {
		return nil, apperr.Forbidden("This token can't upload documents")
	}
	if _, err := s.spaces.Require(ctx, p, in.SpaceID, spaces.ActUpload); err != nil {
		return nil, err
	}
	if free := s.store.FreeBytes(); free >= 0 && free < minFreeBytes {
		return nil, &apperr.Error{Kind: apperr.KindInsufficientStorage, Code: "storage_full",
			Msg: "The server is running out of disk space. Ask an administrator to free up space."}
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
		return nil, apperr.Internal(fmt.Errorf("receive upload: %w", err))
	}
	if n > s.maxUpload {
		return nil, &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large",
			Msg: fmt.Sprintf("This file is larger than the %d MB limit", s.maxUpload>>20)}
	}
	if n == 0 {
		return nil, apperr.Invalid("file", "The file is empty")
	}
	mime, err := Sniff(st.Path())
	if err != nil {
		return nil, err
	}
	if mime == "" || (IsOffice(mime) && !s.OfficeEnabled()) {
		msg := "This file type isn't supported. Upload PDFs, images (JPEG, PNG, TIFF, WebP, HEIC) or text files."
		if IsOffice(mime) {
			msg = "Office documents need the document converter (Gotenberg) to be enabled by an administrator."
		}
		return nil, &apperr.Error{Kind: apperr.KindUnsupported, Code: "unsupported_type", Msg: msg}
	}
	sum := st.SHA256()

	if !in.AllowDuplicate {
		var dupID uuid.UUID
		var dupTitle string
		var dupDeleted bool
		err := s.pool.QueryRow(ctx, `SELECT id, title, deleted_at IS NOT NULL FROM documents WHERE space_id=$1 AND content_hash=$2
			ORDER BY deleted_at NULLS FIRST LIMIT 1`, in.SpaceID, sum).Scan(&dupID, &dupTitle, &dupDeleted)
		if err == nil {
			msg := "This document is already in this space: \"" + dupTitle + "\""
			if dupDeleted {
				msg = "This document is already in this space's Trash: \"" + dupTitle + "\". Restore it instead?"
			}
			return nil, &apperr.Error{Kind: apperr.KindConflict, Code: "duplicate_document", Msg: msg,
				Extra: map[string]any{"document_id": dupID, "title": dupTitle, "in_trash": dupDeleted}}
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}

	blobKey, err := st.Commit()
	if err != nil {
		return nil, fmt.Errorf("store upload: %w", err)
	}
	committed = true

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = TitleFromFilename(in.Filename)
	}
	title = truncate(title, 300)
	source := in.Source
	if source == "" {
		source = "web"
	}
	priority := in.Priority
	if priority == 0 {
		priority = jobs.PriorityInteractive
	}
	id := uuid.Must(uuid.NewV7())
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var lang string
		if in.Language != "" {
			lang = in.Language
		} else if err := tx.QueryRow(ctx, `SELECT default_language FROM spaces WHERE id=$1`, in.SpaceID).Scan(&lang); err != nil {
			return err
		}
		if err := s.validateRefs(ctx, tx, in.SpaceID, in.CorrespondentID, in.DocumentTypeID, in.TagIDs); err != nil {
			return err
		}
		var date any
		if in.DocumentDate != nil {
			if _, err := time.Parse("2006-01-02", *in.DocumentDate); err != nil {
				return apperr.Invalid("document_date", "Use the format YYYY-MM-DD")
			}
			date = *in.DocumentDate
		}
		if _, err := tx.Exec(ctx, `INSERT INTO documents (id, space_id, owner_id, title, document_date, correspondent_id, document_type_id,
			language, content_hash, mime_type, size_bytes, original_filename, source)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			id, in.SpaceID, ownerOf(p), title, date, in.CorrespondentID, in.DocumentTypeID, lang, sum, mime, n,
			truncate(filepath.Base(in.Filename), 255), source); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_files (id, document_id, kind, version_no, blob_key, sha256, mime_type, size_bytes)
			VALUES ($1,$2,'original',1,$3,$4,$5,$6)`, uuid.Must(uuid.NewV7()), id, blobKey, sum, mime, n); err != nil {
			return err
		}
		for _, t := range dedupe(in.TagIDs) {
			if _, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id) VALUES ($1,$2)`, id, t); err != nil {
				return err
			}
		}
		for field, set := range map[string]bool{"title": strings.TrimSpace(in.Title) != "", "correspondent": in.CorrespondentID != nil,
			"document_type": in.DocumentTypeID != nil, "document_date": in.DocumentDate != nil} {
			if set {
				if err := setSource(ctx, tx, id, field, "user", p); err != nil {
					return err
				}
			}
		}
		if err := recordEvent(ctx, tx, id, p, "created", map[string]any{"source": source, "filename": in.Filename}); err != nil {
			return err
		}
		if err := ReindexMeta(ctx, tx, id); err != nil {
			return err
		}
		return s.queue.InsertTx(ctx, tx, jobs.PreprocessArgs{DocumentID: id, Version: 1, Priority: priority},
			&river.InsertOpts{Priority: priority, MaxAttempts: 5})
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func ownerOf(p *auth.Principal) *uuid.UUID {
	if p.UserID == uuid.Nil {
		return nil
	}
	id := p.UserID
	return &id
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// validateRefs ensures referenced taxonomy items belong to the given space.
func (s *Service) validateRefs(ctx context.Context, q db.Querier, spaceID uuid.UUID, corr, typ *uuid.UUID, tags []uuid.UUID) error {
	check := func(table, field string, ids []uuid.UUID) error {
		if len(ids) == 0 {
			return nil
		}
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE space_id=$1 AND id = ANY($2)`, spaceID, dedupe(ids)).Scan(&n); err != nil {
			return err
		}
		if n != len(dedupe(ids)) {
			return apperr.Invalid(field, "Must belong to the document's space")
		}
		return nil
	}
	if corr != nil {
		if err := check("correspondents", "correspondent_id", []uuid.UUID{*corr}); err != nil {
			return err
		}
	}
	if typ != nil {
		if err := check("document_types", "document_type_id", []uuid.UUID{*typ}); err != nil {
			return err
		}
	}
	return check("tags", "tag_ids", tags)
}

func setSource(ctx context.Context, q db.Querier, docID uuid.UUID, field, source string, p *auth.Principal) error {
	_, err := q.Exec(ctx, `INSERT INTO field_sources (document_id, field, source, set_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT (document_id, field) DO UPDATE SET source=excluded.source, set_by=excluded.set_by, set_at=now()`,
		docID, field, source, ownerOf(p))
	return err
}

func recordEvent(ctx context.Context, q db.Querier, docID uuid.UUID, p *auth.Principal, action string, details map[string]any) error {
	actorType := "system"
	if p != nil {
		actorType = string(p.Kind)
	}
	raw, _ := json.Marshal(details)
	_, err := q.Exec(ctx, `INSERT INTO document_events (id, document_id, actor_id, actor_type, action, details) VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.Must(uuid.NewV7()), docID, ownerOf(p), actorType, action, raw)
	return err
}

// RecordEvent is exported for the pipeline (processing milestones in history).
func RecordEvent(ctx context.Context, q db.Querier, docID uuid.UUID, action string, details map[string]any) error {
	return recordEvent(ctx, q, docID, auth.System(), action, details)
}

// ---------------------------------------------------------------------------
// Read
// ---------------------------------------------------------------------------

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Document, error) {
	docs, err := Hydrate(ctx, s.pool, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, apperr.NotFound("Document")
	}
	return docs[0], nil
}

// authorize loads a document and checks the principal's access. Inaccessible
// documents are reported as not found.
func (s *Service) authorize(ctx context.Context, q db.Querier, p *auth.Principal, id uuid.UUID, a spaces.Action, forUpdate bool) (*rawDoc, spaces.Role, error) {
	raw, err := loadRaw(ctx, q, id, forUpdate)
	if db.IsNoRows(err) {
		return nil, "", apperr.NotFound("Document")
	}
	if err != nil {
		return nil, "", err
	}
	role, err := s.spaces.Require(ctx, p, raw.SpaceID, a)
	if err != nil {
		if apperr.IsKind(err, apperr.KindNotFound) {
			return nil, "", apperr.NotFound("Document")
		}
		return nil, "", err
	}
	return raw, role, nil
}

func (s *Service) Get(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Document, error) {
	if _, _, err := s.authorize(ctx, s.pool, p, id, spaces.ActView, false); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

type ListResult struct {
	Items      []*Document `json:"items"`
	Total      *int        `json:"total,omitempty"`
	NextCursor *string     `json:"next_cursor"`
}

// List runs a search and hydrates the results.
func (s *Service) List(ctx context.Context, p *auth.Principal, q search.Query) (*ListResult, error) {
	res, err := s.search.Search(ctx, p, q)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(res.Hits))
	for i, h := range res.Hits {
		ids[i] = h.ID
	}
	docs, err := Hydrate(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	for i, d := range docs {
		// Hydrate preserves order; hits and docs align unless a doc vanished concurrently.
		if i < len(res.Hits) && res.Hits[i].ID == d.ID {
			d.Snippet = res.Hits[i].Snippet
			d.MatchedPage = res.Hits[i].MatchedPage
		}
	}
	return &ListResult{Items: docs, Total: res.Total, NextCursor: res.NextCursor}, nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

type UpdateInput struct {
	Title            opt.Field[string]      `json:"title"`
	DocumentDate     opt.Field[string]      `json:"document_date"`
	CorrespondentID  opt.Field[uuid.UUID]   `json:"correspondent_id"`
	DocumentTypeID   opt.Field[uuid.UUID]   `json:"document_type_id"`
	TagIDs           opt.Field[[]uuid.UUID] `json:"tag_ids"`
	AddTagIDs        []uuid.UUID            `json:"add_tag_ids"`
	RemoveTagIDs     []uuid.UUID            `json:"remove_tag_ids"`
	Language         opt.Field[string]      `json:"language"`
	ASN              opt.Field[int64]       `json:"asn"`
	PhysicalLocation opt.Field[string]      `json:"physical_location"`
	Inbox            opt.Field[bool]        `json:"inbox"`
	SpaceID          opt.Field[uuid.UUID]   `json:"space_id"`
}

var langRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// Update applies a partial update. ifMatch, when non-nil, must equal the current
// version (optimistic concurrency, DESIGN §9.5).
func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in UpdateInput, ifMatch *int) (*Document, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.updateTx(ctx, tx, p, id, in, ifMatch)
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) updateTx(ctx context.Context, tx pgx.Tx, p *auth.Principal, id uuid.UUID, in UpdateInput, ifMatch *int) error {
	raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
	if err != nil {
		return err
	}
	if raw.Deleted {
		return apperr.Conflict("in_trash", "Restore this document from Trash before editing it")
	}
	if ifMatch != nil && *ifMatch != raw.Version {
		return apperr.Precondition("Someone else changed this document. Reload to see their changes.")
	}
	var v apperr.Validation
	sets := []string{}
	args := []any{id}
	arg := func(val any) string {
		args = append(args, val)
		return fmt.Sprintf("$%d", len(args))
	}
	changes := map[string]any{}
	userFields := []string{}

	targetSpace := raw.SpaceID
	moving := in.SpaceID.Set && !in.SpaceID.Null && in.SpaceID.Value != raw.SpaceID
	if in.SpaceID.Set && in.SpaceID.Null {
		v.Add("space_id", "A document must belong to a space")
	}
	if moving {
		if _, err := s.spaces.Require(ctx, p, in.SpaceID.Value, spaces.ActEdit); err != nil {
			return apperr.Invalid("space_id", "You can't add documents to that space")
		}
		targetSpace = in.SpaceID.Value
	}

	if in.Title.Set {
		t := strings.TrimSpace(in.Title.Value)
		if in.Title.Null || t == "" {
			v.Add("title", "Title can't be empty")
		} else {
			t = truncate(t, 300)
			sets = append(sets, "title="+arg(t))
			changes["title"] = t
			userFields = append(userFields, "title")
		}
	}
	if in.DocumentDate.Set {
		if in.DocumentDate.Null {
			sets = append(sets, "document_date=NULL")
			changes["document_date"] = nil
		} else if _, err := time.Parse("2006-01-02", in.DocumentDate.Value); err != nil {
			v.Add("document_date", "Use the format YYYY-MM-DD")
		} else {
			sets = append(sets, "document_date="+arg(in.DocumentDate.Value)+"::date")
			changes["document_date"] = in.DocumentDate.Value
		}
		userFields = append(userFields, "document_date")
	}
	if in.Language.Set && !in.Language.Null {
		if !langRe.MatchString(in.Language.Value) {
			v.Add("language", "Use a language code like en, hi or ta")
		} else {
			sets = append(sets, "language="+arg(in.Language.Value))
			changes["language"] = in.Language.Value
		}
	}
	if in.PhysicalLocation.Set {
		loc := ""
		if !in.PhysicalLocation.Null {
			loc = truncate(strings.TrimSpace(in.PhysicalLocation.Value), 200)
		}
		sets = append(sets, "physical_location="+arg(loc))
		changes["physical_location"] = loc
	}
	if in.Inbox.Set && !in.Inbox.Null {
		sets = append(sets, "inbox="+arg(in.Inbox.Value))
		changes["inbox"] = in.Inbox.Value
	}
	if in.ASN.Set {
		if in.ASN.Null {
			sets = append(sets, "asn=NULL")
		} else if in.ASN.Value <= 0 {
			v.Add("asn", "Must be a positive number")
		} else {
			sets = append(sets, "asn="+arg(in.ASN.Value))
		}
		changes["asn"] = in.ASN.Ptr()
	}

	// Correspondent / type: when moving spaces, map existing values by name.
	corr := in.CorrespondentID
	typ := in.DocumentTypeID
	var mappedTags []uuid.UUID
	if moving {
		if !corr.Set {
			if id, err := mapByName(ctx, tx, "correspondents", "correspondent_id", raw.ID, targetSpace); err != nil {
				return err
			} else if id != nil {
				corr = opt.Of(*id)
			} else {
				corr = opt.Field[uuid.UUID]{Set: true, Null: true}
			}
		}
		if !typ.Set {
			if id, err := mapByName(ctx, tx, "document_types", "document_type_id", raw.ID, targetSpace); err != nil {
				return err
			} else if id != nil {
				typ = opt.Of(*id)
			} else {
				typ = opt.Field[uuid.UUID]{Set: true, Null: true}
			}
		}
		if !in.TagIDs.Set {
			rows, err := tx.Query(ctx, `SELECT t.name FROM document_tags dt JOIN tags t ON t.id=dt.tag_id WHERE dt.document_id=$1`, raw.ID)
			if err != nil {
				return err
			}
			names, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			for _, n := range names {
				tid, err := taxonomy.EnsureByName(ctx, tx, taxonomy.Tags, targetSpace, n)
				if err != nil {
					return err
				}
				mappedTags = append(mappedTags, tid)
			}
			in.TagIDs = opt.Of(mappedTags)
		}
		sets = append(sets, "space_id="+arg(targetSpace))
		changes["space_id"] = targetSpace
	}
	if corr.Set {
		if corr.Null {
			sets = append(sets, "correspondent_id=NULL")
		} else {
			if err := s.validateRefs(ctx, tx, targetSpace, &corr.Value, nil, nil); err != nil {
				return err
			}
			sets = append(sets, "correspondent_id="+arg(corr.Value))
		}
		changes["correspondent_id"] = corr.Ptr()
		if in.CorrespondentID.Set {
			userFields = append(userFields, "correspondent")
		}
	}
	if typ.Set {
		if typ.Null {
			sets = append(sets, "document_type_id=NULL")
		} else {
			if err := s.validateRefs(ctx, tx, targetSpace, nil, &typ.Value, nil); err != nil {
				return err
			}
			sets = append(sets, "document_type_id="+arg(typ.Value))
		}
		changes["document_type_id"] = typ.Ptr()
		if in.DocumentTypeID.Set {
			userFields = append(userFields, "document_type")
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	tagsChanged := false
	if in.TagIDs.Set {
		tags := dedupe(in.TagIDs.Value)
		if in.TagIDs.Null {
			tags = nil
		}
		if err := s.validateRefs(ctx, tx, targetSpace, nil, nil, tags); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM document_tags WHERE document_id=$1 AND NOT (tag_id = ANY($2))`, id, tags); err != nil {
			return err
		}
		for _, t := range tags {
			if _, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, t); err != nil {
				return err
			}
		}
		changes["tag_ids"] = tags
		tagsChanged = true
	}
	if len(in.AddTagIDs) > 0 {
		if err := s.validateRefs(ctx, tx, targetSpace, nil, nil, in.AddTagIDs); err != nil {
			return err
		}
		for _, t := range dedupe(in.AddTagIDs) {
			if _, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, t); err != nil {
				return err
			}
		}
		changes["add_tag_ids"] = in.AddTagIDs
		tagsChanged = true
	}
	if len(in.RemoveTagIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM document_tags WHERE document_id=$1 AND tag_id = ANY($2)`, id, in.RemoveTagIDs); err != nil {
			return err
		}
		changes["remove_tag_ids"] = in.RemoveTagIDs
		tagsChanged = true
	}
	if len(sets) == 0 && !tagsChanged {
		return nil
	}
	sets = append(sets, "version=version+1")
	if _, err := tx.Exec(ctx, `UPDATE documents SET `+strings.Join(sets, ", ")+` WHERE id=$1`, args...); err != nil {
		if db.IsUniqueViolation(err) {
			return apperr.Conflict("asn_taken", "Another document already uses this archive serial number")
		}
		return err
	}
	for _, f := range userFields {
		if err := setSource(ctx, tx, id, f, "user", p); err != nil {
			return err
		}
	}
	if err := recordEvent(ctx, tx, id, p, "updated", changes); err != nil {
		return err
	}
	if err := ReindexMeta(ctx, tx, id); err != nil {
		return err
	}
	if _, ok := changes["language"]; ok {
		return ReindexContent(ctx, tx, id)
	}
	return nil
}

func mapByName(ctx context.Context, tx pgx.Tx, table, col string, docID, target uuid.UUID) (*uuid.UUID, error) {
	var name *string
	if err := tx.QueryRow(ctx, `SELECT x.name FROM documents d LEFT JOIN `+table+` x ON x.id=d.`+col+` WHERE d.id=$1`, docID).Scan(&name); err != nil {
		return nil, err
	}
	if name == nil {
		return nil, nil
	}
	k := taxonomy.Correspondents
	if table == "document_types" {
		k = taxonomy.DocumentTypes
	}
	id, err := taxonomy.EnsureByName(ctx, tx, k, target, *name)
	return &id, err
}

// AssignASN gives the document the next archive serial number if it has none.
func (s *Service) AssignASN(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Document, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true); err != nil {
			return err
		}
		// Serialize assignments; next number is one above the highest in use, so numbers
		// typed in manually are never reused.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7243951)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET asn=(SELECT coalesce(max(asn), 0) + 1 FROM documents), version=version+1
			WHERE id=$1 AND asn IS NULL`, id); err != nil {
			return err
		}
		return recordEvent(ctx, tx, id, p, "asn_assigned", nil)
	})
	if err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

// ---------------------------------------------------------------------------
// Bulk
// ---------------------------------------------------------------------------

type BulkInput struct {
	IDs    []uuid.UUID `json:"ids"`
	Action string      `json:"action"` // update | trash | restore | purge | reprocess
	Update UpdateInput `json:"update"`
}

type BulkResult struct {
	Succeeded int         `json:"succeeded"`
	Failed    []BulkError `json:"failed"`
}

type BulkError struct {
	ID      uuid.UUID `json:"id"`
	Message string    `json:"message"`
}

func (s *Service) Bulk(ctx context.Context, p *auth.Principal, in BulkInput) (*BulkResult, error) {
	if len(in.IDs) == 0 {
		return nil, apperr.Invalid("ids", "Select at least one document")
	}
	if len(in.IDs) > 5000 {
		return nil, apperr.Invalid("ids", "Select at most 5000 documents at a time")
	}
	res := &BulkResult{Failed: []BulkError{}}
	for _, id := range dedupe(in.IDs) {
		var err error
		switch in.Action {
		case "update":
			err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error { return s.updateTx(ctx, tx, p, id, in.Update, nil) })
		case "trash":
			err = s.Trash(ctx, p, id)
		case "restore":
			err = s.Restore(ctx, p, id)
		case "purge":
			err = s.Purge(ctx, p, id)
		case "reprocess":
			err = s.Reprocess(ctx, p, id, "")
		default:
			return nil, apperr.Invalid("action", "Unknown bulk action")
		}
		if err != nil {
			msg := "Failed"
			if ae, ok := apperr.As(err); ok && ae.Kind != apperr.KindInternal {
				msg = ae.Msg
			} else {
				s.log.Error("bulk action failed", "id", id, "action", in.Action, "err", err)
			}
			res.Failed = append(res.Failed, BulkError{ID: id, Message: msg})
			continue
		}
		res.Succeeded++
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Trash
// ---------------------------------------------------------------------------

func (s *Service) Trash(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
		if err != nil {
			return err
		}
		if raw.Deleted {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET deleted_at=now(), version=version+1 WHERE id=$1`, id); err != nil {
			return err
		}
		// Stop outstanding processing work.
		if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='cancelled', updated_at=now() WHERE document_id=$1 AND status IN ('queued','leased')`, id); err != nil {
			return err
		}
		return recordEvent(ctx, tx, id, p, "trashed", nil)
	})
}

func (s *Service) Restore(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
		if err != nil {
			return err
		}
		if !raw.Deleted {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET deleted_at=NULL, version=version+1 WHERE id=$1`, id); err != nil {
			return err
		}
		if raw.Status == "processing" {
			// Processing was cancelled when trashed; start again.
			if err := s.queue.InsertTx(ctx, tx, jobs.PreprocessArgs{DocumentID: id, Version: raw.CurrentVersion},
				&river.InsertOpts{Priority: jobs.PriorityNormal, MaxAttempts: 5}); err != nil {
				return err
			}
		}
		return recordEvent(ctx, tx, id, p, "restored", nil)
	})
}

// Purge permanently deletes a trashed document. Blobs are removed later by garbage
// collection after a grace period (DESIGN §19.1).
func (s *Service) Purge(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		raw, role, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
		if err != nil {
			return err
		}
		if !raw.Deleted {
			return apperr.Conflict("not_in_trash", "Move the document to Trash first")
		}
		isOwner := raw.OwnerID != nil && *raw.OwnerID == p.UserID
		if role != spaces.RoleOwner && !isOwner && p.Kind != auth.KindSystem {
			return apperr.Forbidden("Only the uploader or a space owner can delete documents permanently")
		}
		_, err = tx.Exec(ctx, `DELETE FROM documents WHERE id=$1`, id)
		return err
	})
}

// PurgeExpired permanently deletes documents trashed before the cutoff.
func (s *Service) PurgeExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM documents WHERE deleted_at IS NOT NULL AND deleted_at < $1`, cutoff)
	return tag.RowsAffected(), err
}

// Reprocess schedules a fresh processing run (OCR, indexing, classification).
func (s *Service) Reprocess(ctx context.Context, p *auth.Principal, id uuid.UUID, profile string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActEdit, true)
		if err != nil {
			return err
		}
		if raw.Deleted {
			return apperr.Conflict("in_trash", "Restore this document from Trash first")
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='cancelled', updated_at=now()
			WHERE document_id=$1 AND status IN ('queued','leased')`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET status='processing', processing_stage='queued', processing_error='' WHERE id=$1`, id); err != nil {
			return err
		}
		if err := recordEvent(ctx, tx, id, p, "reprocess_requested", map[string]any{"profile": profile}); err != nil {
			return err
		}
		return s.queue.InsertTx(ctx, tx, jobs.PreprocessArgs{DocumentID: id, Version: raw.CurrentVersion, Profile: profile, Priority: jobs.PriorityNormal},
			&river.InsertOpts{Priority: jobs.PriorityNormal, MaxAttempts: 5})
	})
}

// ---------------------------------------------------------------------------
// Files & text
// ---------------------------------------------------------------------------

type File struct {
	Reader   io.ReadSeekCloser
	Size     int64
	Mime     string
	Name     string
	ETag     string
	Modified time.Time
}

// OpenFile opens the original, archive or thumbnail of the current version.
func (s *Service) OpenFile(ctx context.Context, p *auth.Principal, id uuid.UUID, kind string) (*File, error) {
	if _, _, err := s.authorize(ctx, s.pool, p, id, spaces.ActView, false); err != nil {
		return nil, err
	}
	var key, mime, title, orig string
	var sha []byte
	var created time.Time
	err := s.pool.QueryRow(ctx, `SELECT f.blob_key, f.mime_type, f.sha256, f.created_at, d.title, d.original_filename
		FROM document_files f JOIN documents d ON d.id=f.document_id
		WHERE f.document_id=$1 AND f.kind=$2 AND f.version_no=d.current_version`, id, kind).Scan(&key, &mime, &sha, &created, &title, &orig)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("File")
	}
	if err != nil {
		return nil, err
	}
	r, size, err := s.store.Open(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		s.log.Error("blob missing", "document", id, "kind", kind, "key", key)
		return nil, apperr.NotFound("File")
	}
	if err != nil {
		return nil, err
	}
	name := orig
	if kind != "original" || name == "" {
		name = title + ExtFor(mime)
	}
	return &File{Reader: r, Size: size, Mime: mime, Name: name, ETag: fmt.Sprintf(`"%x"`, sha[:12]), Modified: created}, nil
}

type Page struct {
	PageNo     int      `json:"page_no"`
	Text       string   `json:"text"`
	Confidence *float32 `json:"confidence"`
	Rotation   int      `json:"rotation"`
}

func (s *Service) Pages(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]Page, error) {
	if _, _, err := s.authorize(ctx, s.pool, p, id, spaces.ActView, false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT page_no, text, confidence, rotation FROM pages WHERE document_id=$1 ORDER BY page_no`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Page, error) {
		var x Page
		err := r.Scan(&x.PageNo, &x.Text, &x.Confidence, &x.Rotation)
		return x, err
	})
}

// ---------------------------------------------------------------------------
// Notes & history
// ---------------------------------------------------------------------------

type Note struct {
	ID        uuid.UUID   `json:"id"`
	Author    *Ref        `json:"author"`
	Body      string      `json:"body"`
	Mentions  []uuid.UUID `json:"mentions"`
	CreatedAt time.Time   `json:"created_at"`
	CanDelete bool        `json:"can_delete"`
}

func (s *Service) Notes(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]Note, error) {
	_, role, err := s.authorize(ctx, s.pool, p, id, spaces.ActView, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id, n.author_id, u.display_name, n.body, n.mentions, n.created_at
		FROM notes n LEFT JOIN users u ON u.id=n.author_id WHERE n.document_id=$1 ORDER BY n.created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Note, error) {
		var n Note
		var aid *uuid.UUID
		var aname *string
		err := r.Scan(&n.ID, &aid, &aname, &n.Body, &n.Mentions, &n.CreatedAt)
		if aid != nil && aname != nil {
			n.Author = &Ref{ID: *aid, Name: *aname}
		}
		n.CanDelete = role == spaces.RoleOwner || (aid != nil && *aid == p.UserID)
		return n, err
	})
}

var mentionRe = regexp.MustCompile(`@\[[^\]]{1,80}\]\(([0-9a-fA-F-]{36})\)`)

// AddNote adds a comment. Any space member (including viewers) may comment.
// Mentions use the markup @[Name](user-id) produced by the UI.
func (s *Service) AddNote(ctx context.Context, p *auth.Principal, id uuid.UUID, body string) (*Note, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > 5000 {
		return nil, apperr.Invalid("body", "A note must be 1–5000 characters")
	}
	if p.Kind == auth.KindToken && !p.Has(auth.ScopeWrite) {
		return nil, apperr.Forbidden("")
	}
	n := &Note{ID: uuid.Must(uuid.NewV7()), Body: body, CreatedAt: time.Now(), CanDelete: true, Mentions: []uuid.UUID{}}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		raw, _, err := s.authorize(ctx, tx, p, id, spaces.ActView, false)
		if err != nil {
			return err
		}
		for _, m := range mentionRe.FindAllStringSubmatch(body, 20) {
			uid, err := uuid.Parse(m[1])
			if err != nil || uid == p.UserID || slices.Contains(n.Mentions, uid) {
				continue
			}
			if role, err := s.spaces.RoleOf(ctx, tx, uid, raw.SpaceID); err == nil && role != "" {
				n.Mentions = append(n.Mentions, uid)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO notes (id, document_id, author_id, body, mentions) VALUES ($1,$2,$3,$4,$5)`,
			n.ID, id, p.UserID, body, n.Mentions); err != nil {
			return err
		}
		if err := ReindexMeta(ctx, tx, id); err != nil {
			return err
		}
		if len(n.Mentions) > 0 {
			docID := id
			plain := mentionRe.ReplaceAllStringFunc(body, func(m string) string {
				return "@" + strings.SplitN(strings.TrimPrefix(m, "@["), "]", 2)[0]
			})
			return s.queue.EmitTx(ctx, tx, jobs.Event{Type: "note.mention", Title: p.Name + " mentioned you on \"" + raw.Title + "\"",
				Body: truncate(plain, 300), Link: "/documents/" + id.String(), Recipients: n.Mentions, DocumentID: &docID, ActorID: ownerOf(p)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	n.Author = &Ref{ID: p.UserID, Name: p.Name}
	return n, nil
}

func (s *Service) DeleteNote(ctx context.Context, p *auth.Principal, docID, noteID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, role, err := s.authorize(ctx, tx, p, docID, spaces.ActView, false)
		if err != nil {
			return err
		}
		var author *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT author_id FROM notes WHERE id=$1 AND document_id=$2`, noteID, docID).Scan(&author); err != nil {
			if db.IsNoRows(err) {
				return apperr.NotFound("Note")
			}
			return err
		}
		if role != spaces.RoleOwner && (author == nil || *author != p.UserID) {
			return apperr.Forbidden("You can only delete your own notes")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notes WHERE id=$1`, noteID); err != nil {
			return err
		}
		return ReindexMeta(ctx, tx, docID)
	})
}

type HistoryEntry struct {
	ID        uuid.UUID      `json:"id"`
	Actor     *Ref           `json:"actor"`
	ActorType string         `json:"actor_type"`
	Action    string         `json:"action"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"created_at"`
}

func (s *Service) History(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]HistoryEntry, error) {
	if _, _, err := s.authorize(ctx, s.pool, p, id, spaces.ActView, false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.actor_id, u.display_name, e.actor_type, e.action, e.details, e.created_at
		FROM document_events e LEFT JOIN users u ON u.id=e.actor_id WHERE e.document_id=$1 ORDER BY e.created_at DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryEntry, error) {
		var h HistoryEntry
		var aid *uuid.UUID
		var aname *string
		var raw []byte
		err := r.Scan(&h.ID, &aid, &aname, &h.ActorType, &h.Action, &raw, &h.CreatedAt)
		if aid != nil && aname != nil {
			h.Actor = &Ref{ID: *aid, Name: *aname}
		}
		_ = json.Unmarshal(raw, &h.Details)
		if h.Details == nil {
			h.Details = map[string]any{}
		}
		return h, err
	})
}

// Stats for the home dashboard.
type Stats struct {
	Total      int   `json:"total"`
	Inbox      int   `json:"inbox"`
	Processing int   `json:"processing"`
	Failed     int   `json:"failed"`
	Trash      int   `json:"trash"`
	AddedWeek  int   `json:"added_this_week"`
	Bytes      int64 `json:"bytes"`
}

func (s *Service) Stats(ctx context.Context, p *auth.Principal) (*Stats, error) {
	ids, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	var st Stats
	err = s.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE deleted_at IS NULL),
		count(*) FILTER (WHERE deleted_at IS NULL AND inbox),
		count(*) FILTER (WHERE deleted_at IS NULL AND status='processing'),
		count(*) FILTER (WHERE deleted_at IS NULL AND status IN ('failed','needs_password')),
		count(*) FILTER (WHERE deleted_at IS NOT NULL),
		count(*) FILTER (WHERE deleted_at IS NULL AND added_at > now() - interval '7 days'),
		coalesce(sum(size_bytes) FILTER (WHERE deleted_at IS NULL), 0)
		FROM documents WHERE space_id = ANY($1)`, ids).Scan(&st.Total, &st.Inbox, &st.Processing, &st.Failed, &st.Trash, &st.AddedWeek, &st.Bytes)
	return &st, err
}
