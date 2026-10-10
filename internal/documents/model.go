// Package documents implements documents: ingestion, metadata, files, notes, history,
// trash and indexing of metadata for search.
package documents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/textindex"
)

type Ref struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color,omitempty"`
}

type Document struct {
	ID               uuid.UUID            `json:"id"`
	Space            Ref                  `json:"space"`
	Title            string               `json:"title"`
	DocumentDate     *string              `json:"document_date"` // YYYY-MM-DD
	AddedAt          time.Time            `json:"added_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
	Correspondent    *Ref                 `json:"correspondent"`
	DocumentType     *Ref                 `json:"document_type"`
	Tags             []Ref                `json:"tags"`
	Language         string               `json:"language"`
	PageCount        *int                 `json:"page_count"`
	ASN              *int64               `json:"asn"`
	PhysicalLocation string               `json:"physical_location"`
	Inbox            bool                 `json:"inbox"`
	Status           string               `json:"status"`
	ProcessingStage  string               `json:"processing_stage"`
	ProcessingError  string               `json:"processing_error,omitempty"`
	Progress         *Progress            `json:"progress,omitempty"` // while text is being read
	MimeType         string               `json:"mime_type"`
	SizeBytes        int64                `json:"size_bytes"`
	OriginalFilename string               `json:"original_filename"`
	Source           string               `json:"source"`
	Owner            *Ref                 `json:"owner"`
	HasArchive       bool                 `json:"has_archive"`
	HasThumbnail     bool                 `json:"has_thumbnail"`
	HasDerived       bool                 `json:"has_derived"` // a browser-friendly working copy (HEIC converted to JPEG)
	CustomFields     []customfields.Value `json:"custom_fields"`
	SuggestionCount  int                  `json:"suggestion_count"` // open AI suggestions
	NoteCount        int                  `json:"note_count"`
	Version          int                  `json:"version"`
	DeletedAt        *time.Time           `json:"deleted_at,omitempty"`

	// Present in search results only.
	Snippet     []textindex.Segment `json:"snippet,omitempty"`
	MatchedPage *int                `json:"matched_page,omitempty"`
}

// Progress of reading a document's text. PagesTotal is 0 when only the worker knows the page
// count (a multi-page TIFF, say). Workers report pages done with every heartbeat.
type Progress struct {
	PagesDone  int `json:"pages_done"`
	PagesTotal int `json:"pages_total"`
}

const hydrateSQL = `SELECT d.id, d.space_id, s.name, s.color, d.title, to_char(d.document_date, 'YYYY-MM-DD'), d.added_at, d.updated_at,
	d.correspondent_id, c.name, d.document_type_id, t.name,
	coalesce((SELECT json_agg(json_build_object('id', tg.id, 'name', tg.name, 'color', tg.color) ORDER BY lower(tg.name))
		FROM document_tags dt JOIN tags tg ON tg.id = dt.tag_id WHERE dt.document_id = d.id), '[]'),
	d.language, d.page_count, d.asn, d.physical_location, d.inbox, d.status, d.processing_stage, d.processing_error,
	d.mime_type, d.size_bytes, d.original_filename, d.source, d.owner_id, u.display_name,
	EXISTS (SELECT 1 FROM document_files f WHERE f.document_id = d.id AND f.kind = 'archive' AND f.version_no = d.current_version),
	EXISTS (SELECT 1 FROM document_files f WHERE f.document_id = d.id AND f.kind = 'thumbnail' AND f.version_no = d.current_version),
	EXISTS (SELECT 1 FROM document_files f WHERE f.document_id = d.id AND f.kind = 'derived' AND f.version_no = d.current_version),
	(SELECT count(*) FROM notes n WHERE n.document_id = d.id), d.version, d.deleted_at,
	(SELECT count(*) FROM suggestions sg WHERE sg.document_id = d.id AND sg.status = 'pending'),
	CASE WHEN d.status = 'processing' THEN (
		SELECT json_build_object('pages_total', r.pages_total, 'pages_done', r.pages_total
			- coalesce(sum(pt.page_to - pt.page_from + 1), 0) -- pages still to read (whole-file tasks have no range)
			+ coalesce(sum(CASE WHEN pt.status = 'leased' AND jsonb_typeof(pt.progress->'pages_done') = 'number'
				THEN (pt.progress->>'pages_done')::numeric END), 0))
		FROM ocr_runs r
		LEFT JOIN processing_tasks pt ON pt.ocr_run_id = r.id AND pt.type = 'ocr' AND pt.status IN ('queued', 'leased')
		WHERE r.document_id = d.id AND r.status = 'running'
		GROUP BY r.id ORDER BY r.created_at DESC LIMIT 1) END
	FROM documents d
	JOIN spaces s ON s.id = d.space_id
	LEFT JOIN correspondents c ON c.id = d.correspondent_id
	LEFT JOIN document_types t ON t.id = d.document_type_id
	LEFT JOIN users u ON u.id = d.owner_id`

func scanDocument(row pgx.Row) (*Document, error) {
	var d Document
	var corrID, typeID, ownerID *uuid.UUID
	var corrName, typeName, ownerName *string
	var tags, progress []byte
	err := row.Scan(&d.ID, &d.Space.ID, &d.Space.Name, &d.Space.Color, &d.Title, &d.DocumentDate, &d.AddedAt, &d.UpdatedAt,
		&corrID, &corrName, &typeID, &typeName, &tags,
		&d.Language, &d.PageCount, &d.ASN, &d.PhysicalLocation, &d.Inbox, &d.Status, &d.ProcessingStage, &d.ProcessingError,
		&d.MimeType, &d.SizeBytes, &d.OriginalFilename, &d.Source, &ownerID, &ownerName,
		&d.HasArchive, &d.HasThumbnail, &d.HasDerived, &d.NoteCount, &d.Version, &d.DeletedAt, &d.SuggestionCount, &progress)
	if err != nil {
		return nil, err
	}
	if progress != nil {
		var p struct {
			PagesDone  float64 `json:"pages_done"`
			PagesTotal float64 `json:"pages_total"`
		}
		if json.Unmarshal(progress, &p) == nil {
			d.Progress = &Progress{PagesDone: max(0, int(p.PagesDone)), PagesTotal: int(p.PagesTotal)}
			if p.PagesTotal > 0 {
				d.Progress.PagesDone = min(d.Progress.PagesDone, d.Progress.PagesTotal)
			}
		}
	}
	if corrID != nil && corrName != nil {
		d.Correspondent = &Ref{ID: *corrID, Name: *corrName}
	}
	if typeID != nil && typeName != nil {
		d.DocumentType = &Ref{ID: *typeID, Name: *typeName}
	}
	if ownerID != nil && ownerName != nil {
		d.Owner = &Ref{ID: *ownerID, Name: *ownerName}
	}
	d.Tags = []Ref{}
	if err := json.Unmarshal(tags, &d.Tags); err != nil {
		return nil, err
	}
	return &d, nil
}

// Hydrate loads full documents for ids, preserving order and skipping missing ones.
func Hydrate(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]*Document, error) {
	if len(ids) == 0 {
		return []*Document{}, nil
	}
	rows, err := q.Query(ctx, hydrateSQL+` WHERE d.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[uuid.UUID]*Document, len(ids))
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		byID[d.ID] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Document, 0, len(ids))
	for _, id := range ids {
		if d, ok := byID[id]; ok {
			out = append(out, d)
		}
	}
	values, err := customfields.Load(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	for _, d := range out {
		d.CustomFields = values[d.ID]
		if d.CustomFields == nil {
			d.CustomFields = []customfields.Value{}
		}
	}
	return out, nil
}

// rawDoc is the minimal row used for authorization and mutation checks.
type rawDoc struct {
	ID             uuid.UUID
	SpaceID        uuid.UUID
	OwnerID        *uuid.UUID
	Version        int
	CurrentVersion int
	Status         string
	MimeType       string
	Title          string
	Deleted        bool
}

func loadRaw(ctx context.Context, q db.Querier, id uuid.UUID, forUpdate bool) (*rawDoc, error) {
	sql := `SELECT id, space_id, owner_id, version, current_version, status, mime_type, title, deleted_at IS NOT NULL FROM documents WHERE id=$1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	var r rawDoc
	err := q.QueryRow(ctx, sql, id).Scan(&r.ID, &r.SpaceID, &r.OwnerID, &r.Version, &r.CurrentVersion, &r.Status, &r.MimeType, &r.Title, &r.Deleted)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
