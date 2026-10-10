package exchange

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/storage"
)

// Report says what an import did.
type Report struct {
	Users      int      `json:"users"`
	Spaces     int      `json:"spaces"`
	Documents  int      `json:"documents"`
	Skipped    int      `json:"skipped"` // already there (same id) or an identical file already in the space
	Notes      int      `json:"notes"`
	Warnings   []string `json:"warnings"`
	UnsetUsers []string `json:"users_without_password"`
}

type importer struct {
	pool  *pgxpool.Pool
	store storage.Store
	dir   string
	rep   *Report

	users, spaces, tags, corrs, types, fields map[uuid.UUID]uuid.UUID
}

func (im *importer) warn(format string, args ...any) {
	im.rep.Warnings = append(im.rep.Warnings, fmt.Sprintf(format, args...))
}

// Import reads an export folder. It can be run again safely: whatever is already there
// is left alone.
func Import(ctx context.Context, pool *pgxpool.Pool, store storage.Store, dir string) (*Report, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("this doesn't look like a Docveta export (no manifest.json): %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if m.Schema != Schema {
		return nil, fmt.Errorf("unsupported export format %q (this version reads %s)", m.Schema, Schema)
	}
	im := &importer{pool: pool, store: store, dir: dir, rep: &Report{Warnings: []string{}, UnsetUsers: []string{}},
		users: map[uuid.UUID]uuid.UUID{}, spaces: map[uuid.UUID]uuid.UUID{}, tags: map[uuid.UUID]uuid.UUID{}, corrs: map[uuid.UUID]uuid.UUID{},
		types: map[uuid.UUID]uuid.UUID{}, fields: map[uuid.UUID]uuid.UUID{}}
	if err := db.InTx(ctx, pool, func(tx pgx.Tx) error { return im.structure(ctx, tx, &m) }); err != nil {
		return nil, fmt.Errorf("users, spaces and vocabulary: %w", err)
	}

	f, err := os.Open(filepath.Join(dir, "documents.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("documents.jsonl: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	for n := 1; sc.Scan(); n++ {
		var d DocRec
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			im.warn("documents.jsonl line %d: %v", n, err)
			continue
		}
		if err := im.document(ctx, &d); err != nil {
			im.warn("document %q: %v", d.Title, err)
		}
	}
	if err := sc.Err(); err != nil {
		return im.rep, err
	}
	if err := db.InTx(ctx, pool, func(tx pgx.Tx) error { return im.views(ctx, tx, &m) }); err != nil {
		im.warn("saved views: %v", err)
	}
	return im.rep, nil
}

func (im *importer) structure(ctx context.Context, tx pgx.Tx, m *Manifest) error {
	// Users: matched by email; new ones get no password unless the export carries hashes.
	for _, u := range m.Users {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, strings.ToLower(u.Email)).Scan(&id)
		if err == nil {
			im.users[u.ID] = id
			continue
		}
		if !db.IsNoRows(err) {
			return err
		}
		id = u.ID
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&taken); err != nil {
			return err
		}
		if taken {
			id = uuid.Must(uuid.NewV7())
		}
		if _, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name, password_hash, is_admin, status, locale, timezone, date_format)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, strings.ToLower(u.Email), u.DisplayName, u.PasswordHash, u.IsAdmin, u.Status, u.Locale, u.Timezone, u.DateFormat); err != nil {
			return err
		}
		im.users[u.ID] = id
		im.rep.Users++
		if u.PasswordHash == nil {
			im.rep.UnsetUsers = append(im.rep.UnsetUsers, u.Email)
		}
	}
	// Spaces.
	for _, s := range m.Spaces {
		var id uuid.UUID
		if s.Kind == "personal" {
			owner := uuid.Nil
			for _, mem := range s.Members {
				if mem.Role == "owner" {
					owner = im.users[mem.UserID]
				}
			}
			if owner == uuid.Nil {
				im.warn("personal space %q has no known owner; skipped", s.Name)
				continue
			}
			err := tx.QueryRow(ctx, `SELECT s.id FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$1 WHERE s.kind='personal' LIMIT 1`, owner).Scan(&id)
			if db.IsNoRows(err) {
				id = s.ID
				if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, description, color, default_language, ai_policy, ai_apply_mode, created_by, ai_new_tags)
					VALUES ($1,$2,'personal',$3,$4,$5,$6,$7,$8,coalesce($9,true)) ON CONFLICT (id) DO NOTHING`, id, s.Name, s.Description, s.Color, s.DefaultLanguage, s.AIPolicy, s.AIApplyMode, owner, s.AINewTags); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE spaces SET ai_auto_confidence=coalesce($2,ai_auto_confidence), ai_new_confidence=coalesce($3,ai_new_confidence),
					ai_max_new_tags=coalesce($4,ai_max_new_tags), ai_new_types=coalesce($5,ai_new_types) WHERE id=$1`, id, s.AIAutoConfidence, s.AINewConfidence, s.AIMaxNewTags, s.AINewTypes); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,'owner') ON CONFLICT DO NOTHING`, id, owner); err != nil {
					return err
				}
				im.rep.Spaces++
			} else if err != nil {
				return err
			}
		} else {
			err := tx.QueryRow(ctx, `SELECT id FROM spaces WHERE id=$1`, s.ID).Scan(&id)
			if db.IsNoRows(err) {
				id = s.ID
				if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, description, color, default_language, ai_policy, ai_apply_mode, ai_new_tags)
					VALUES ($1,$2,'shared',$3,$4,$5,$6,$7,coalesce($8,true))`, id, s.Name, s.Description, s.Color, s.DefaultLanguage, s.AIPolicy, s.AIApplyMode, s.AINewTags); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE spaces SET ai_auto_confidence=coalesce($2,ai_auto_confidence), ai_new_confidence=coalesce($3,ai_new_confidence),
					ai_max_new_tags=coalesce($4,ai_max_new_tags), ai_new_types=coalesce($5,ai_new_types) WHERE id=$1`, id, s.AIAutoConfidence, s.AINewConfidence, s.AIMaxNewTags, s.AINewTypes); err != nil {
					return err
				}
				im.rep.Spaces++
			} else if err != nil {
				return err
			}
			for _, mem := range s.Members {
				if uid, ok := im.users[mem.UserID]; ok {
					if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, id, uid, mem.Role); err != nil {
						return err
					}
				}
			}
		}
		im.spaces[s.ID] = id
	}
	// Vocabulary and fields: matched by name within the space.
	vocab := func(table string, recs []VocabRec, into map[uuid.UUID]uuid.UUID) error {
		for _, v := range recs {
			sp, ok := im.spaces[v.SpaceID]
			if !ok {
				continue
			}
			var id uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE space_id=$1 AND lower(name)=lower($2)`, sp, v.Name).Scan(&id)
			if db.IsNoRows(err) {
				id = v.ID
				var taken bool
				_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, id).Scan(&taken)
				if taken {
					id = uuid.Must(uuid.NewV7())
				}
				if table == "tags" {
					_, err = tx.Exec(ctx, `INSERT INTO tags (id, space_id, name, color, match_algorithm, match_pattern, case_sensitive) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
						id, sp, v.Name, coalesce(v.Color, "slate"), v.MatchAlgorithm, v.MatchPattern, v.CaseSensitive)
				} else {
					_, err = tx.Exec(ctx, `INSERT INTO `+table+` (id, space_id, name, match_algorithm, match_pattern, case_sensitive) VALUES ($1,$2,$3,$4,$5,$6)`,
						id, sp, v.Name, v.MatchAlgorithm, v.MatchPattern, v.CaseSensitive)
				}
			}
			if err != nil {
				return err
			}
			into[v.ID] = id
		}
		return nil
	}
	if err := vocab("tags", m.Tags, im.tags); err != nil {
		return err
	}
	if err := vocab("correspondents", m.Correspondents, im.corrs); err != nil {
		return err
	}
	if err := vocab("document_types", m.DocumentTypes, im.types); err != nil {
		return err
	}
	for _, f := range m.CustomFields {
		sp, ok := im.spaces[f.SpaceID]
		if !ok {
			continue
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM custom_fields WHERE space_id=$1 AND lower(name)=lower($2)`, sp, f.Name).Scan(&id)
		if db.IsNoRows(err) {
			id = f.ID
			var taken bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM custom_fields WHERE id=$1)`, id).Scan(&taken)
			if taken {
				id = uuid.Must(uuid.NewV7())
			}
			_, err = tx.Exec(ctx, `INSERT INTO custom_fields (id, space_id, name, data_type, options) VALUES ($1,$2,$3,$4,$5)`, id, sp, f.Name, f.DataType, []byte(f.Options))
		}
		if err != nil {
			return err
		}
		im.fields[f.ID] = id
	}
	// Anyone left without a personal space (an export from a partial database) gets one.
	for _, u := range m.Users {
		if id, ok := im.users[u.ID]; ok {
			var has bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$1 WHERE s.kind='personal')`, id).Scan(&has); err != nil {
				return err
			}
			if !has {
				if _, err := spaces.CreatePersonal(ctx, tx, id, u.DisplayName); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// stage copies a file of the export into the store and checks its checksum.
func (im *importer) stage(rel, wantSHA string) (key string, sum []byte, size int64, err error) {
	if rel == "" {
		return "", nil, 0, errors.New("no file")
	}
	clean := filepath.Join(im.dir, filepath.FromSlash(rel))
	if root, e := filepath.Abs(im.dir); e == nil {
		if abs, e := filepath.Abs(clean); e != nil || !strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return "", nil, 0, fmt.Errorf("%q is outside the export folder", rel)
		}
	}
	f, err := os.Open(clean)
	if err != nil {
		return "", nil, 0, err
	}
	defer f.Close()
	st, err := im.store.Stage()
	if err != nil {
		return "", nil, 0, err
	}
	if _, err := io.Copy(st, f); err != nil {
		st.Discard()
		return "", nil, 0, err
	}
	sum, size = st.SHA256(), st.Size()
	if wantSHA != "" && hex.EncodeToString(sum) != wantSHA {
		st.Discard()
		return "", nil, 0, errors.New("the file's checksum doesn't match the manifest (damaged export?)")
	}
	key, err = st.Commit()
	return key, sum, size, err
}

func (im *importer) document(ctx context.Context, d *DocRec) error {
	space, ok := im.spaces[d.SpaceID]
	if !ok {
		return errors.New("its space isn't in the export")
	}
	var exists bool
	if err := im.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE id=$1) OR EXISTS(SELECT 1 FROM documents WHERE space_id=$2 AND content_hash=decode($3,'hex'))`,
		d.ID, space, d.SHA256).Scan(&exists); err != nil {
		return err
	}
	if exists {
		im.rep.Skipped++
		return nil
	}
	origKey, origSum, size, err := im.stage(d.Original, d.SHA256)
	if err != nil {
		return err
	}
	var archKey, thumbKey string
	var archSum, thumbSum []byte
	var archSize, thumbSize int64
	if d.Archive != "" {
		if archKey, archSum, archSize, err = im.stage(d.Archive, ""); err != nil {
			im.warn("document %q: archive skipped: %v", d.Title, err)
			archKey = ""
		}
	}
	if d.Thumbnail != "" {
		if thumbKey, thumbSum, thumbSize, err = im.stage(d.Thumbnail, ""); err != nil {
			thumbKey = ""
		}
	}
	status, stage, perr := d.Status, "done", d.ProcessingError
	switch status {
	case "ready":
	case "failed", "needs_password":
		stage = status
	default:
		status, stage, perr = "failed", "failed", "This document was exported before processing finished. Use Process again."
	}
	mapID := func(m map[uuid.UUID]uuid.UUID, id *uuid.UUID) *uuid.UUID {
		if id == nil {
			return nil
		}
		if v, ok := m[*id]; ok {
			return &v
		}
		return nil
	}
	var owner *uuid.UUID
	if d.OwnerID != nil {
		if v, ok := im.users[*d.OwnerID]; ok {
			owner = &v
		}
	}
	return db.InTx(ctx, im.pool, func(tx pgx.Tx) error {
		var asn *int64
		if d.ASN != nil {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE asn=$1)`, *d.ASN).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				asn = d.ASN
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO documents (id, space_id, owner_id, title, document_date, added_at, correspondent_id, document_type_id, language,
			page_count, asn, physical_location, inbox, status, processing_stage, processing_error, current_version, content_hash, mime_type, size_bytes,
			original_filename, source) VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,1,$17,$18,$19,$20,$21)`,
			d.ID, space, owner, d.Title, d.DocumentDate, d.AddedAt, mapID(im.corrs, d.CorrespondentID), mapID(im.types, d.DocumentTypeID), d.Language,
			d.PageCount, asn, d.PhysicalLocation, d.Inbox, status, stage, perr, origSum, d.MimeType, size, d.OriginalFilename, coalesce(d.Source, "import")); err != nil {
			return err
		}
		files := []struct {
			kind, key, mime string
			sum             []byte
			size            int64
		}{{"original", origKey, d.MimeType, origSum, size}}
		if archKey != "" {
			files = append(files, struct {
				kind, key, mime string
				sum             []byte
				size            int64
			}{"archive", archKey, documents.MimePDF, archSum, archSize})
		}
		if thumbKey != "" {
			files = append(files, struct {
				kind, key, mime string
				sum             []byte
				size            int64
			}{"thumbnail", thumbKey, "image/jpeg", thumbSum, thumbSize})
		}
		for _, f := range files {
			if _, err := tx.Exec(ctx, `INSERT INTO document_files (id, document_id, kind, version_no, blob_key, sha256, mime_type, size_bytes) VALUES ($1,$2,$3,1,$4,$5,$6,$7)`,
				uuid.Must(uuid.NewV7()), d.ID, f.kind, f.key, f.sum, f.mime, f.size); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO document_versions (document_id, version_no, note) VALUES ($1,1,'Imported')`, d.ID); err != nil {
			return err
		}
		for _, t := range d.TagIDs {
			if id, ok := im.tags[t]; ok {
				if _, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id, source) VALUES ($1,$2,'user') ON CONFLICT DO NOTHING`, d.ID, id); err != nil {
					return err
				}
			}
		}
		for _, p := range d.Pages {
			if _, err := tx.Exec(ctx, `INSERT INTO pages (document_id, page_no, text, confidence, rotation) VALUES ($1,$2,$3,$4,$5)`, d.ID, p.No, p.Text, p.Confidence, p.Rotation); err != nil {
				return err
			}
		}
		for _, n := range d.Notes {
			var author *uuid.UUID
			if n.AuthorID != nil {
				if v, ok := im.users[*n.AuthorID]; ok {
					author = &v
				}
			}
			if _, err := tx.Exec(ctx, `INSERT INTO notes (id, document_id, author_id, body, created_at) VALUES ($1,$2,$3,$4,$5)`, uuid.Must(uuid.NewV7()), d.ID, author, n.Body, n.CreatedAt); err != nil {
				return err
			}
			im.rep.Notes++
		}
		if len(d.CustomFields) > 0 {
			vals := map[string]json.RawMessage{}
			for _, c := range d.CustomFields {
				if id, ok := im.fields[c.FieldID]; ok {
					b, _ := json.Marshal(c.Value)
					vals[id.String()] = b
				}
			}
			sp, err := tx.Begin(ctx) // a value that no longer fits is skipped, not fatal
			if err != nil {
				return err
			}
			if _, err := customfields.SetValues(ctx, sp, d.ID, space, vals, "import"); err != nil {
				_ = sp.Rollback(ctx)
				im.warn("document %q: custom fields skipped: %v", d.Title, err)
			} else if err := sp.Commit(ctx); err != nil {
				return err
			}
		}
		if err := documents.RecordEvent(ctx, tx, d.ID, "imported", map[string]any{"source": "export"}); err != nil {
			return err
		}
		if err := documents.ReindexContent(ctx, tx, d.ID); err != nil {
			return err
		}
		if err := documents.ReindexMeta(ctx, tx, d.ID); err != nil {
			return err
		}
		im.rep.Documents++
		return nil
	})
}

func (im *importer) views(ctx context.Context, tx pgx.Tx, m *Manifest) error {
	for _, v := range m.SavedViews {
		owner, ok := im.users[v.OwnerID]
		if !ok {
			continue
		}
		var sp *uuid.UUID
		if v.SpaceID != nil {
			id, ok := im.spaces[*v.SpaceID]
			if !ok {
				continue
			}
			sp = &id
		}
		if _, err := tx.Exec(ctx, `INSERT INTO saved_views (id, space_id, owner_id, name, query, pinned)
			SELECT $1,$2,$3,$4,$5,$6 WHERE NOT EXISTS (SELECT 1 FROM saved_views WHERE id=$1 OR (owner_id=$3 AND name=$4))`,
			v.ID, sp, owner, v.Name, []byte(v.Query), v.Pinned); err != nil {
			return err
		}
	}
	return nil
}
