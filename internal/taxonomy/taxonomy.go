// Package taxonomy manages per-space vocabularies: tags, correspondents and document
// types, including their auto-matching rules (DESIGN §5.2, §9.2).
package taxonomy

import (
	"context"
	"regexp"
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

// Kind identifies a vocabulary table.
type Kind string

const (
	Tags           Kind = "tags"
	Correspondents Kind = "correspondents"
	DocumentTypes  Kind = "document_types"
)

func (k Kind) Valid() bool { return k == Tags || k == Correspondents || k == DocumentTypes }

func (k Kind) label() string {
	switch k {
	case Tags:
		return "Tag"
	case Correspondents:
		return "Correspondent"
	}
	return "Document type"
}

// docColumn is the documents column referencing this kind (tags use a join table).
func (k Kind) docColumn() string {
	switch k {
	case Correspondents:
		return "correspondent_id"
	case DocumentTypes:
		return "document_type_id"
	}
	return ""
}

type Item struct {
	ID             uuid.UUID `json:"id"`
	SpaceID        uuid.UUID `json:"space_id"`
	Name           string    `json:"name"`
	Color          string    `json:"color,omitempty"`
	MatchAlgorithm string    `json:"match_algorithm"`
	MatchPattern   string    `json:"match_pattern"`
	CaseSensitive  bool      `json:"case_sensitive"`
	DocumentCount  int       `json:"document_count"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Input struct {
	Name           *string `json:"name"`
	Color          *string `json:"color"`
	MatchAlgorithm *string `json:"match_algorithm"`
	MatchPattern   *string `json:"match_pattern"`
	CaseSensitive  *bool   `json:"case_sensitive"`
}

var tagColors = map[string]bool{"slate": true, "red": true, "orange": true, "amber": true, "lime": true, "green": true,
	"teal": true, "cyan": true, "blue": true, "indigo": true, "violet": true, "pink": true}

func (in *Input) validate(create bool) error {
	var v apperr.Validation
	if in.Name != nil {
		n := strings.Join(strings.Fields(*in.Name), " ")
		in.Name = &n
		if n == "" || utf8.RuneCountInString(n) > 100 {
			v.Add("name", "Name must be 1–100 characters")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	if in.Color != nil && !tagColors[*in.Color] {
		v.Add("color", "Unknown color")
	}
	if in.MatchAlgorithm != nil {
		switch *in.MatchAlgorithm {
		case "none", "any", "all", "exact", "regex", "fuzzy":
		default:
			v.Add("match_algorithm", "Unknown matching method")
		}
		if *in.MatchAlgorithm == "regex" && in.MatchPattern != nil {
			if _, err := regexp.Compile(*in.MatchPattern); err != nil {
				v.Add("match_pattern", "Invalid regular expression: %v", err)
			}
		}
	}
	if in.MatchPattern != nil && len(*in.MatchPattern) > 1000 {
		v.Add("match_pattern", "Pattern is too long")
	}
	return v.Err()
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service) *Service {
	return &Service{pool: pool, spaces: sp}
}

func (s *Service) countExpr(k Kind) string {
	if k == Tags {
		return `(SELECT count(*) FROM document_tags dt JOIN documents d ON d.id=dt.document_id
			WHERE dt.tag_id=t.id AND d.deleted_at IS NULL)`
	}
	return `(SELECT count(*) FROM documents d WHERE d.` + k.docColumn() + `=t.id AND d.deleted_at IS NULL)`
}

func colorCol(k Kind) string {
	if k == Tags {
		return "t.color"
	}
	return "''"
}

func (s *Service) scan(row pgx.Row) (*Item, error) {
	var it Item
	err := row.Scan(&it.ID, &it.SpaceID, &it.Name, &it.Color, &it.MatchAlgorithm, &it.MatchPattern, &it.CaseSensitive, &it.DocumentCount, &it.UpdatedAt)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Item")
	}
	return &it, err
}

func (s *Service) selectSQL(k Kind) string {
	return `SELECT t.id, t.space_id, t.name, ` + colorCol(k) + `, t.match_algorithm, t.match_pattern, t.case_sensitive, ` +
		s.countExpr(k) + `, t.updated_at FROM ` + string(k) + ` t`
}

// List returns all items of a kind in the given spaces (that the caller can see).
func (s *Service) List(ctx context.Context, p *auth.Principal, k Kind, spaceID *uuid.UUID) ([]*Item, error) {
	var ids []uuid.UUID
	if spaceID != nil {
		if _, err := s.spaces.Require(ctx, p, *spaceID, spaces.ActView); err != nil {
			return nil, err
		}
		ids = []uuid.UUID{*spaceID}
	} else {
		var err error
		if ids, err = s.spaces.VisibleSpaceIDs(ctx, p.UserID); err != nil {
			return nil, err
		}
	}
	// Count documents with one grouped pass over the spaces' documents instead of a count per
	// item: with thousands of tags or senders that is the difference between one scan and thousands.
	counts := `SELECT d.` + k.docColumn() + ` AS id, count(*) AS n FROM documents d
		WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND d.` + k.docColumn() + ` IS NOT NULL GROUP BY 1`
	if k == Tags {
		counts = `SELECT dt.tag_id AS id, count(*) AS n FROM document_tags dt JOIN documents d ON d.id=dt.document_id
			WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL GROUP BY 1`
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.space_id, t.name, `+colorCol(k)+`, t.match_algorithm, t.match_pattern, t.case_sensitive,
			coalesce(c.n, 0), t.updated_at
		FROM `+string(k)+` t LEFT JOIN (`+counts+`) c ON c.id = t.id
		WHERE t.space_id = ANY($1) ORDER BY lower(t.name)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Item{}
	for rows.Next() {
		it, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Service) get(ctx context.Context, k Kind, id uuid.UUID) (*Item, error) {
	it, err := s.scan(s.pool.QueryRow(ctx, s.selectSQL(k)+` WHERE t.id=$1`, id))
	if apperr.IsKind(err, apperr.KindNotFound) {
		return nil, apperr.NotFound(k.label())
	}
	return it, err
}

func (s *Service) Get(ctx context.Context, p *auth.Principal, k Kind, id uuid.UUID) (*Item, error) {
	it, err := s.get(ctx, k, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.spaces.Require(ctx, p, it.SpaceID, spaces.ActView); err != nil {
		return nil, apperr.NotFound(k.label())
	}
	return it, nil
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, k Kind, spaceID uuid.UUID, in Input) (*Item, error) {
	if _, err := s.spaces.Require(ctx, p, spaceID, spaces.ActEdit); err != nil {
		return nil, err
	}
	if err := in.validate(true); err != nil {
		return nil, err
	}
	id, err := s.insert(ctx, s.pool, k, spaceID, in)
	if err != nil {
		return nil, err
	}
	return s.get(ctx, k, id)
}

func (s *Service) insert(ctx context.Context, q db.Querier, k Kind, spaceID uuid.UUID, in Input) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	var err error
	if k == Tags {
		_, err = q.Exec(ctx, `INSERT INTO tags (id, space_id, name, color, match_algorithm, match_pattern, case_sensitive)
			VALUES ($1,$2,$3,coalesce($4,'slate'),coalesce($5,'none'),coalesce($6,''),coalesce($7,false))`,
			id, spaceID, *in.Name, in.Color, in.MatchAlgorithm, in.MatchPattern, in.CaseSensitive)
	} else {
		_, err = q.Exec(ctx, `INSERT INTO `+string(k)+` (id, space_id, name, match_algorithm, match_pattern, case_sensitive)
			VALUES ($1,$2,$3,coalesce($4,'none'),coalesce($5,''),coalesce($6,false))`,
			id, spaceID, *in.Name, in.MatchAlgorithm, in.MatchPattern, in.CaseSensitive)
	}
	if db.IsUniqueViolation(err) {
		return uuid.Nil, apperr.Conflict("name_taken", k.label()+" \""+*in.Name+"\" already exists in this space")
	}
	return id, err
}

// EnsureByName returns the id of the item with the given name in the space, creating it
// if missing. Used when moving documents between spaces and by importers.
func EnsureByName(ctx context.Context, q db.Querier, k Kind, spaceID uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM `+string(k)+` WHERE space_id=$1 AND lower(name)=lower($2)`, spaceID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !db.IsNoRows(err) {
		return uuid.Nil, err
	}
	id = uuid.Must(uuid.NewV7())
	err = q.QueryRow(ctx, `INSERT INTO `+string(k)+` (id, space_id, name) VALUES ($1,$2,$3)
		ON CONFLICT (space_id, lower(name)) DO UPDATE SET name=`+string(k)+`.name RETURNING id`, id, spaceID, name).Scan(&id)
	return id, err
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, k Kind, id uuid.UUID, in Input) (*Item, error) {
	it, err := s.Get(ctx, p, k, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.spaces.Require(ctx, p, it.SpaceID, spaces.ActEdit); err != nil {
		return nil, err
	}
	if in.MatchAlgorithm != nil && *in.MatchAlgorithm == "regex" && in.MatchPattern == nil {
		in.MatchPattern = &it.MatchPattern
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	var q string
	args := []any{id, in.Name, in.MatchAlgorithm, in.MatchPattern, in.CaseSensitive}
	if k == Tags {
		q = `UPDATE tags SET name=coalesce($2,name), match_algorithm=coalesce($3,match_algorithm),
			match_pattern=coalesce($4,match_pattern), case_sensitive=coalesce($5,case_sensitive), color=coalesce($6,color) WHERE id=$1`
		args = append(args, in.Color)
	} else {
		q = `UPDATE ` + string(k) + ` SET name=coalesce($2,name), match_algorithm=coalesce($3,match_algorithm),
			match_pattern=coalesce($4,match_pattern), case_sensitive=coalesce($5,case_sensitive) WHERE id=$1`
	}
	if _, err := s.pool.Exec(ctx, q, args...); err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("name_taken", k.label()+" with this name already exists in this space")
		}
		return nil, err
	}
	if in.Name != nil {
		// Names are part of documents' search vectors.
		if err := s.markDocumentsForReindex(ctx, k, id); err != nil {
			return nil, err
		}
	}
	return s.get(ctx, k, id)
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, k Kind, id uuid.UUID) error {
	it, err := s.Get(ctx, p, k, id)
	if err != nil {
		return err
	}
	if _, err := s.spaces.Require(ctx, p, it.SpaceID, spaces.ActEdit); err != nil {
		return err
	}
	if err := s.markDocumentsForReindex(ctx, k, id); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM `+string(k)+` WHERE id=$1`, id)
	return err
}

// Merge moves all documents from the source items into target and deletes the sources.
func (s *Service) Merge(ctx context.Context, p *auth.Principal, k Kind, target uuid.UUID, sources []uuid.UUID) (*Item, error) {
	t, err := s.Get(ctx, p, k, target)
	if err != nil {
		return nil, err
	}
	if _, err := s.spaces.Require(ctx, p, t.SpaceID, spaces.ActEdit); err != nil {
		return nil, err
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, src := range sources {
			if src == target {
				continue
			}
			var sp uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT space_id FROM `+string(k)+` WHERE id=$1`, src).Scan(&sp); err != nil {
				if db.IsNoRows(err) {
					return apperr.NotFound(k.label())
				}
				return err
			}
			if sp != t.SpaceID {
				return apperr.Invalid("sources", "Only items from the same space can be merged")
			}
			if k == Tags {
				if _, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id, source)
					SELECT document_id, $1, source FROM document_tags WHERE tag_id=$2 ON CONFLICT DO NOTHING`, target, src); err != nil {
					return err
				}
			} else {
				if _, err := tx.Exec(ctx, `UPDATE documents SET `+k.docColumn()+`=$1 WHERE `+k.docColumn()+`=$2`, target, src); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM `+string(k)+` WHERE id=$1`, src); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.markDocumentsForReindex(ctx, k, target); err != nil {
		return nil, err
	}
	return s.get(ctx, k, target)
}

// ReindexHook is called with document ids whose metadata vectors must be rebuilt.
var ReindexHook func(ctx context.Context, docIDs []uuid.UUID) error

func (s *Service) markDocumentsForReindex(ctx context.Context, k Kind, id uuid.UUID) error {
	if ReindexHook == nil {
		return nil
	}
	var q string
	if k == Tags {
		q = `SELECT document_id FROM document_tags WHERE tag_id=$1`
	} else {
		q = `SELECT id FROM documents WHERE ` + k.docColumn() + `=$1`
	}
	rows, err := s.pool.Query(ctx, q, id)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) == 0 {
		return err
	}
	return ReindexHook(ctx, ids)
}
