// Package collections groups related documents by hand ("Car papers", "House purchase"),
// across spaces: personal, or shared with a space like saved views.
package collections

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/spaces"
)

type Collection struct {
	ID          uuid.UUID  `json:"id"`
	SpaceID     *uuid.UUID `json:"space_id"` // nil = personal
	OwnerID     uuid.UUID  `json:"owner_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Color       string     `json:"color"`
	// DocumentCount counts the documents in it that the caller can see.
	DocumentCount int `json:"document_count"`
	// Covers are up to four of those documents, last added first, for the card's thumbnails.
	Covers    []uuid.UUID `json:"covers"`
	CanEdit   bool        `json:"can_edit"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Input struct {
	// SpaceID shares the collection with a space; the zero UUID makes it personal again.
	SpaceID     *uuid.UUID `json:"space_id"`
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Color       *string    `json:"color"`
}

var colors = []string{"slate", "red", "orange", "amber", "green", "teal", "cyan", "blue", "indigo", "violet", "pink"}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service) *Service {
	return &Service{pool: pool, spaces: sp}
}

// List returns the collections the caller can see; with docID, only those holding that document.
func (s *Service) List(ctx context.Context, p *auth.Principal, docID *uuid.UUID) ([]Collection, error) {
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.space_id, c.owner_id, c.name, c.description, c.color, c.updated_at,
			(c.space_id IS NULL AND c.owner_id = $1) OR coalesce(m.role IN ('owner','editor'), false),
			(SELECT count(*) FROM collection_items i JOIN documents d ON d.id = i.document_id
				WHERE i.collection_id = c.id AND d.deleted_at IS NULL AND d.space_id = ANY($2)),
			ARRAY(SELECT d.id FROM collection_items i JOIN documents d ON d.id = i.document_id
				WHERE i.collection_id = c.id AND d.deleted_at IS NULL AND d.space_id = ANY($2) ORDER BY i.added_at DESC LIMIT 4)
		FROM collections c LEFT JOIN space_members m ON m.space_id = c.space_id AND m.user_id = $1
		WHERE ((c.space_id IS NULL AND c.owner_id = $1) OR m.user_id IS NOT NULL)
			AND ($3::uuid IS NULL OR EXISTS (SELECT 1 FROM collection_items i WHERE i.collection_id = c.id AND i.document_id = $3))
		ORDER BY lower(c.name)`, p.UserID, visible, docID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Collection, error) {
		var c Collection
		err := r.Scan(&c.ID, &c.SpaceID, &c.OwnerID, &c.Name, &c.Description, &c.Color, &c.UpdatedAt, &c.CanEdit, &c.DocumentCount, &c.Covers)
		return c, err
	})
}

// Get returns one collection the caller can see.
func (s *Service) Get(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Collection, error) {
	list, err := s.List(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, apperr.NotFound("Collection")
}

func (s *Service) editable(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Collection, error) {
	c, err := s.Get(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if !c.CanEdit {
		return nil, apperr.Forbidden("You can only view this collection")
	}
	return c, nil
}

func (in *Input) validate(create bool) error {
	var v apperr.Validation
	if in.Name != nil {
		n := strings.Join(strings.Fields(*in.Name), " ")
		in.Name = &n
		if n == "" || utf8.RuneCountInString(n) > 80 {
			v.Add("name", "Name must be 1–80 characters")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		in.Description = &d
		if utf8.RuneCountInString(d) > 500 {
			v.Add("description", "At most 500 characters")
		}
	}
	if in.Color != nil && !slices.Contains(colors, *in.Color) {
		v.Add("color", "Unknown colour")
	}
	return v.Err()
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, in Input) (*Collection, error) {
	if err := in.validate(true); err != nil {
		return nil, err
	}
	if in.SpaceID != nil && *in.SpaceID == uuid.Nil {
		in.SpaceID = nil
	}
	if in.SpaceID != nil {
		if _, err := s.spaces.Require(ctx, p, *in.SpaceID, spaces.ActEdit); err != nil {
			return nil, err
		}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO collections (id, space_id, owner_id, name, description, color)
		VALUES ($1,$2,$3,$4,coalesce($5,''),coalesce($6,'blue'))`, id, in.SpaceID, p.UserID, *in.Name, in.Description, in.Color); err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// Update renames, describes or recolours a collection, or shares it with another space
// (needs edit rights there too) or makes it personal (it becomes the caller's).
func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*Collection, error) {
	c, err := s.editable(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	space := c.SpaceID
	if in.SpaceID != nil {
		if *in.SpaceID == uuid.Nil {
			space = nil
		} else {
			if _, err := s.spaces.Require(ctx, p, *in.SpaceID, spaces.ActEdit); err != nil {
				return nil, err
			}
			space = in.SpaceID
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE collections SET name=coalesce($2,name), description=coalesce($3,description),
		color=coalesce($4,color), space_id=$5, owner_id=CASE WHEN $5::uuid IS NULL THEN $6 ELSE owner_id END, updated_at=now() WHERE id=$1`,
		id, in.Name, in.Description, in.Color, space, p.UserID); err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if _, err := s.editable(ctx, p, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM collections WHERE id=$1`, id)
	return err
}

// Add puts documents the caller can see into the collection; others are skipped. It
// returns how many were added (ones already in it don't count).
func (s *Service) Add(ctx context.Context, p *auth.Principal, id uuid.UUID, docs []uuid.UUID) (int, error) {
	if _, err := s.editable(ctx, p, id); err != nil {
		return 0, err
	}
	if len(docs) == 0 || len(docs) > 5000 {
		return 0, apperr.Invalid("ids", "Choose 1–5000 documents")
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO collection_items (collection_id, document_id, added_by)
		SELECT $1, d.id, $4 FROM documents d WHERE d.id = ANY($2) AND d.space_id = ANY($3) AND d.deleted_at IS NULL
		ON CONFLICT DO NOTHING`, id, docs, visible, p.UserID)
	if err != nil {
		return 0, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE collections SET updated_at=now() WHERE id=$1`, id)
	return int(tag.RowsAffected()), nil
}

// Remove takes documents out of the collection (the documents themselves stay).
func (s *Service) Remove(ctx context.Context, p *auth.Principal, id uuid.UUID, docs []uuid.UUID) (int, error) {
	if _, err := s.editable(ctx, p, id); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM collection_items WHERE collection_id=$1 AND document_id = ANY($2)`, id, docs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
