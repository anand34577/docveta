// Package spaces implements Spaces, memberships and the single authorization policy
// used by every other package.
package spaces

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool { return r == RoleOwner || r == RoleEditor || r == RoleViewer }

// Action is something a principal wants to do in a space.
type Action int

const (
	ActView   Action = iota // read documents, search, download, comment
	ActEdit                 // upload, edit metadata, tag, trash
	ActManage               // members, vocabulary, settings, delete space
	ActUpload               // add documents: role as ActEdit, but also allowed for upload-only tokens
)

func (r Role) Allows(a Action) bool {
	switch a {
	case ActView:
		return r.Valid()
	case ActEdit, ActUpload:
		return r == RoleOwner || r == RoleEditor
	case ActManage:
		return r == RoleOwner
	}
	return false
}

type Space struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	Description     string    `json:"description"`
	Color           string    `json:"color"`
	AIPolicy        string    `json:"ai_policy"`
	AIApplyMode     string    `json:"ai_apply_mode"`
	DefaultLanguage string    `json:"default_language"`
	// Batch scanning: split a scanned batch at separator sheets; read ASN labels.
	SplitOnSeparators bool      `json:"split_on_separators"`
	ReadASNBarcodes   bool      `json:"read_asn_barcodes"`
	Role              Role      `json:"role"` // the caller's role
	MemberCount       int       `json:"member_count"`
	DocumentCount     int       `json:"document_count"`
	CreatedAt         time.Time `json:"created_at"`
}

type Member struct {
	UserID      uuid.UUID `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        Role      `json:"role"`
	AddedAt     time.Time `json:"added_at"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

// RoleOf returns the user's role in a space, or "" if not a member.
func (s *Service) RoleOf(ctx context.Context, q db.Querier, userID, spaceID uuid.UUID) (Role, error) {
	var r Role
	err := q.QueryRow(ctx, `SELECT role FROM space_members WHERE space_id=$1 AND user_id=$2`, spaceID, userID).Scan(&r)
	if db.IsNoRows(err) {
		return "", nil
	}
	return r, err
}

// Require returns nil when the principal may perform action a in the space. Missing
// spaces and spaces the user cannot see both return NotFound to avoid leaking existence.
func (s *Service) Require(ctx context.Context, p *auth.Principal, spaceID uuid.UUID, a Action) (Role, error) {
	if p == nil {
		return "", apperr.Unauthorized("")
	}
	if p.Kind == auth.KindSystem {
		return RoleOwner, nil
	}
	scopeOK := p.Has(auth.ScopeWrite)
	switch a {
	case ActView:
		scopeOK = p.Has(auth.ScopeRead)
	case ActUpload:
		scopeOK = scopeOK || p.Has(auth.ScopeUpload)
	}
	if !scopeOK {
		return "", apperr.Forbidden("This token doesn't have the required scope")
	}
	r, err := s.RoleOf(ctx, s.pool, p.UserID, spaceID)
	if err != nil {
		return "", err
	}
	if r == "" {
		return "", apperr.NotFound("Space")
	}
	if !r.Allows(a) {
		switch a {
		case ActEdit, ActUpload:
			return r, apperr.Forbidden("You can view this space but not change it")
		default:
			return r, apperr.Forbidden("Only space owners can do this")
		}
	}
	return r, nil
}

// VisibleSpaceIDs lists the spaces whose documents the user can see.
func (s *Service) VisibleSpaceIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT space_id FROM space_members WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

const spaceCols = `s.id, s.name, s.kind, s.description, s.color, s.ai_policy, s.ai_apply_mode, s.default_language,
	s.split_on_separators, s.read_asn_barcodes, m.role, (SELECT count(*) FROM space_members x WHERE x.space_id=s.id),
	(SELECT count(*) FROM documents d WHERE d.space_id=s.id AND d.deleted_at IS NULL), s.created_at`

func scanSpace(row pgx.Row) (*Space, error) {
	var sp Space
	err := row.Scan(&sp.ID, &sp.Name, &sp.Kind, &sp.Description, &sp.Color, &sp.AIPolicy, &sp.AIApplyMode,
		&sp.DefaultLanguage, &sp.SplitOnSeparators, &sp.ReadASNBarcodes, &sp.Role, &sp.MemberCount, &sp.DocumentCount, &sp.CreatedAt)
	return &sp, err
}

// List returns the spaces the user belongs to, personal space first.
func (s *Service) List(ctx context.Context, p *auth.Principal) ([]*Space, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+spaceCols+`
		FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$1
		ORDER BY (s.kind='personal') DESC, lower(s.name)`, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Space
	for rows.Next() {
		sp, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Space, error) {
	if _, err := s.Require(ctx, p, id, ActView); err != nil {
		return nil, err
	}
	sp, err := scanSpace(s.pool.QueryRow(ctx, `SELECT `+spaceCols+`
		FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$2 WHERE s.id=$1`, id, p.UserID))
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Space")
	}
	return sp, err
}

type Input struct {
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	Color           *string `json:"color"`
	AIPolicy        *string `json:"ai_policy"`
	AIApplyMode     *string `json:"ai_apply_mode"`
	DefaultLanguage *string `json:"default_language"`

	SplitOnSeparators *bool `json:"split_on_separators"`
	ReadASNBarcodes   *bool `json:"read_asn_barcodes"`
}

func (in *Input) validate(create bool) error {
	var v apperr.Validation
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" || len(n) > 80 {
			v.Add("name", "Name must be 1–80 characters")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	if in.AIPolicy != nil && *in.AIPolicy != "off" && *in.AIPolicy != "local_only" && *in.AIPolicy != "any" {
		v.Add("ai_policy", "Must be off, local_only or any")
	}
	if in.AIApplyMode != nil && *in.AIApplyMode != "suggest" && *in.AIApplyMode != "auto" {
		v.Add("ai_apply_mode", "Must be suggest or auto")
	}
	if in.Description != nil && len(*in.Description) > 500 {
		v.Add("description", "Description is too long")
	}
	return v.Err()
}

// Create makes a shared space owned by the caller.
func (s *Service) Create(ctx context.Context, p *auth.Principal, in Input) (*Space, error) {
	if p == nil || !p.Has(auth.ScopeWrite) {
		return nil, apperr.Forbidden("")
	}
	if err := in.validate(true); err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, description, color, ai_policy, ai_apply_mode, default_language, created_by)
			VALUES ($1,$2,'shared',coalesce($3,''),coalesce($4,'indigo'),coalesce($5,'off'),coalesce($6,'suggest'),coalesce($7,'en'),$8)`,
			id, *in.Name, in.Description, in.Color, in.AIPolicy, in.AIApplyMode, in.DefaultLanguage, p.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,'owner')`, id, p.UserID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// CreatePersonal creates the personal space for a new user inside an existing transaction.
func CreatePersonal(ctx context.Context, tx pgx.Tx, userID uuid.UUID, displayName string) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "Personal"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, created_by) VALUES ($1,$2,'personal',$3)`, id, name, userID); err != nil {
		return uuid.Nil, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,'owner')`, id, userID)
	return id, err
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*Space, error) {
	if _, err := s.Require(ctx, p, id, ActManage); err != nil {
		return nil, err
	}
	if err := in.validate(false); err != nil {
		return nil, err
	}
	_, err := s.pool.Exec(ctx, `UPDATE spaces SET
		name=coalesce($2,name), description=coalesce($3,description), color=coalesce($4,color),
		ai_policy=coalesce($5,ai_policy), ai_apply_mode=coalesce($6,ai_apply_mode), default_language=coalesce($7,default_language),
		split_on_separators=coalesce($8,split_on_separators), read_asn_barcodes=coalesce($9,read_asn_barcodes)
		WHERE id=$1`, id, in.Name, in.Description, in.Color, in.AIPolicy, in.AIApplyMode, in.DefaultLanguage, in.SplitOnSeparators, in.ReadASNBarcodes)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// Delete removes an empty shared space. Spaces with documents must be emptied first
// (documents moved or deleted) so nothing is lost by accident.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	sp, err := s.Get(ctx, p, id)
	if err != nil {
		return err
	}
	if sp.Role != RoleOwner {
		return apperr.Forbidden("Only space owners can do this")
	}
	if sp.Kind == "personal" {
		return apperr.Conflict("personal_space", "Personal spaces can't be deleted")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM documents WHERE space_id=$1`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return apperr.Conflict("space_not_empty", "Move or delete the documents in this space first (including Trash)")
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM spaces WHERE id=$1`, id)
	return err
}

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

func (s *Service) Members(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]Member, error) {
	if _, err := s.Require(ctx, p, id, ActView); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.display_name, m.role, m.created_at
		FROM space_members m JOIN users u ON u.id=m.user_id WHERE m.space_id=$1
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, lower(u.display_name)`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		err := r.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.AddedAt)
		return m, err
	})
}

// SetMember adds a member or changes their role.
func (s *Service) SetMember(ctx context.Context, p *auth.Principal, spaceID, userID uuid.UUID, role Role) error {
	sp, err := s.Get(ctx, p, spaceID)
	if err != nil {
		return err
	}
	if sp.Role != RoleOwner {
		return apperr.Forbidden("Only space owners can manage members")
	}
	if !role.Valid() {
		return apperr.Invalid("role", "Must be owner, editor or viewer")
	}
	if sp.Kind == "personal" && userID != p.UserID {
		return apperr.Conflict("personal_space", "Personal spaces can't be shared. Create a shared space instead, or share individual documents.")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND status='active')`, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound("User")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,$3)
			ON CONFLICT (space_id, user_id) DO UPDATE SET role=excluded.role`, spaceID, userID, role); err != nil {
			return err
		}
		return ensureOwner(ctx, tx, spaceID)
	})
}

func (s *Service) RemoveMember(ctx context.Context, p *auth.Principal, spaceID, userID uuid.UUID) error {
	sp, err := s.Get(ctx, p, spaceID)
	if err != nil {
		return err
	}
	// Members may leave by themselves; owners may remove anyone.
	if sp.Role != RoleOwner && userID != p.UserID {
		return apperr.Forbidden("Only space owners can manage members")
	}
	if sp.Kind == "personal" {
		return apperr.Conflict("personal_space", "You can't leave your personal space")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM space_members WHERE space_id=$1 AND user_id=$2`, spaceID, userID); err != nil {
			return err
		}
		return ensureOwner(ctx, tx, spaceID)
	})
}

func ensureOwner(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) error {
	var owners int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM space_members WHERE space_id=$1 AND role='owner'`, spaceID).Scan(&owners); err != nil {
		return err
	}
	if owners == 0 {
		return apperr.Conflict("last_owner", "A space needs at least one owner. Make someone else an owner first.")
	}
	return nil
}
