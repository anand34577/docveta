// Package views implements saved views ("smart folders"): a saved search with display
// options, personal or shared with a space (DESIGN FR-O4).
package views

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
)

type View struct {
	ID        uuid.UUID       `json:"id"`
	SpaceID   *uuid.UUID      `json:"space_id"` // nil = personal
	OwnerID   uuid.UUID       `json:"owner_id"`
	Name      string          `json:"name"`
	Query     json.RawMessage `json:"query"`
	Display   json.RawMessage `json:"display"`
	Pinned    bool            `json:"pinned"`
	SortOrder int             `json:"sort_order"`
	CanEdit   bool            `json:"can_edit"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Input struct {
	SpaceID   *uuid.UUID      `json:"space_id"`
	Name      *string         `json:"name"`
	Query     json.RawMessage `json:"query"`
	Display   json.RawMessage `json:"display"`
	Pinned    *bool           `json:"pinned"`
	SortOrder *int            `json:"sort_order"`
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service) *Service {
	return &Service{pool: pool, spaces: sp}
}

func (s *Service) List(ctx context.Context, p *auth.Principal) ([]View, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.id, v.space_id, v.owner_id, v.name, v.query, v.display, v.pinned, v.sort_order, v.updated_at,
		v.owner_id = $1 OR coalesce(m.role IN ('owner','editor'), false)
		FROM saved_views v LEFT JOIN space_members m ON m.space_id = v.space_id AND m.user_id = $1
		WHERE v.owner_id = $1 OR m.user_id IS NOT NULL
		ORDER BY v.sort_order, lower(v.name)`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (View, error) {
		var v View
		err := r.Scan(&v.ID, &v.SpaceID, &v.OwnerID, &v.Name, &v.Query, &v.Display, &v.Pinned, &v.SortOrder, &v.UpdatedAt, &v.CanEdit)
		return v, err
	})
}

func validJSONObject(raw json.RawMessage) bool {
	var m map[string]any
	return json.Unmarshal(raw, &m) == nil
}

func (in *Input) validate(create bool) error {
	var v apperr.Validation
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" || utf8.RuneCountInString(n) > 80 {
			v.Add("name", "Name must be 1–80 characters")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	if in.Query != nil && (!validJSONObject(in.Query) || len(in.Query) > 16<<10) {
		v.Add("query", "Invalid query")
	}
	if in.Display != nil && (!validJSONObject(in.Display) || len(in.Display) > 4<<10) {
		v.Add("display", "Invalid display settings")
	}
	return v.Err()
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, in Input) (*View, error) {
	if err := in.validate(true); err != nil {
		return nil, err
	}
	if in.SpaceID != nil {
		if _, err := s.spaces.Require(ctx, p, *in.SpaceID, spaces.ActEdit); err != nil {
			return nil, err
		}
	}
	if in.Query == nil {
		in.Query = json.RawMessage(`{}`)
	}
	if in.Display == nil {
		in.Display = json.RawMessage(`{}`)
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO saved_views (id, space_id, owner_id, name, query, display, pinned, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,coalesce($7,true),coalesce($8,(SELECT coalesce(max(sort_order),0)+1 FROM saved_views WHERE owner_id=$3)))`,
		id, in.SpaceID, p.UserID, *in.Name, in.Query, in.Display, in.Pinned, in.SortOrder); err != nil {
		return nil, err
	}
	return s.get(ctx, p, id)
}

func (s *Service) get(ctx context.Context, p *auth.Principal, id uuid.UUID) (*View, error) {
	list, err := s.List(ctx, p)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, apperr.NotFound("Saved view")
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*View, error) {
	v, err := s.get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if !v.CanEdit {
		return nil, apperr.Forbidden("You can't change this view")
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `UPDATE saved_views SET name=coalesce($2,name), query=coalesce($3,query), display=coalesce($4,display),
		pinned=coalesce($5,pinned), sort_order=coalesce($6,sort_order), updated_at=now() WHERE id=$1`,
		id, in.Name, nullRaw(in.Query), nullRaw(in.Display), in.Pinned, in.SortOrder)
	if err != nil {
		return nil, err
	}
	return s.get(ctx, p, id)
}

func nullRaw(r json.RawMessage) any {
	if r == nil {
		return nil
	}
	return []byte(r)
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	v, err := s.get(ctx, p, id)
	if err != nil {
		return err
	}
	if !v.CanEdit {
		return apperr.Forbidden("You can't delete this view")
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM saved_views WHERE id=$1`, id)
	return err
}

// ---------------------------------------------------------------------------
// Delta sync for offline-capable clients (DESIGN §21.3)
// ---------------------------------------------------------------------------

type Change struct {
	Seq     int64           `json:"seq"`
	Entity  string          `json:"entity"`
	ID      uuid.UUID       `json:"id"`
	SpaceID *uuid.UUID      `json:"space_id"`
	Deleted bool            `json:"deleted"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type ChangesPage struct {
	Changes []Change `json:"changes"`
	Next    int64    `json:"next_since"`
	More    bool     `json:"has_more"`
}

// Changes returns entities changed after `since` in the caller's spaces. Clients store
// next_since and call again. Documents return their id only (fetch details on demand).
func Changes(ctx context.Context, q db.Querier, sp *spaces.Service, p *auth.Principal, since int64, limit int) (*ChangesPage, error) {
	ids, err := sp.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT * FROM (
			SELECT change_seq, 'document' AS entity, id, space_id, false AS deleted, NULL::jsonb AS data FROM documents
				WHERE space_id = ANY($1) AND change_seq > $2
			UNION ALL SELECT change_seq, 'tag', id, space_id, false, jsonb_build_object('name', name, 'color', color) FROM tags
				WHERE space_id = ANY($1) AND change_seq > $2
			UNION ALL SELECT change_seq, 'correspondent', id, space_id, false, jsonb_build_object('name', name) FROM correspondents
				WHERE space_id = ANY($1) AND change_seq > $2
			UNION ALL SELECT change_seq, 'document_type', id, space_id, false, jsonb_build_object('name', name) FROM document_types
				WHERE space_id = ANY($1) AND change_seq > $2
			UNION ALL SELECT change_seq, entity_type, entity_id, space_id, true, NULL FROM tombstones
				WHERE (space_id = ANY($1) OR space_id IS NULL) AND change_seq > $2
			-- Documents moved out of a space the client can see. If the client can see the new space too,
			-- the document's own change entry covers it (a delete would arrive after the update and win).
			UNION ALL SELECT r.change_seq, r.entity_type, r.entity_id, r.space_id, true, NULL FROM space_removals r
				WHERE r.space_id = ANY($1) AND r.change_seq > $2
				  AND NOT EXISTS (SELECT 1 FROM documents d WHERE d.id = r.entity_id AND d.space_id = ANY($1))
		) c ORDER BY change_seq LIMIT $3`, ids, since, limit+1)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Change, error) {
		var c Change
		err := r.Scan(&c.Seq, &c.Entity, &c.ID, &c.SpaceID, &c.Deleted, &c.Data)
		return c, err
	})
	if err != nil {
		return nil, err
	}
	page := &ChangesPage{Changes: list, Next: since}
	if len(list) > limit {
		page.Changes = list[:limit]
		page.More = true
	}
	if n := len(page.Changes); n > 0 {
		page.Next = page.Changes[n-1].Seq
	}
	return page, nil
}
