// Package spaces implements Spaces, memberships and the single authorization policy
// used by every other package.
package spaces

import (
	"context"
	"maps"
	"strings"
	"sync"
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
	AINewTags       bool      `json:"ai_new_tags"` // the AI may propose tags that don't exist yet
	DefaultLanguage string    `json:"default_language"`
	// Fine-tuning of the AI for this space. Confidences are percentages.
	AIAutoConfidence int  `json:"ai_auto_confidence"` // how sure before "apply automatically" applies
	AINewConfidence  int  `json:"ai_new_confidence"`  // how sure before proposing a name that doesn't exist yet
	AIMaxNewTags     int  `json:"ai_max_new_tags"`    // new tags per document
	AINewTypes       bool `json:"ai_new_types"`       // may propose document types the space doesn't have
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
	pool   *pgxpool.Pool
	countM sync.Mutex
	counts map[uuid.UUID]Totals
	forgot uint64 // ForgetTotals calls so far
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

const spaceCols = `s.id, s.name, s.kind, s.description, s.color, s.ai_policy, s.ai_apply_mode, s.ai_new_tags, s.default_language,
	s.split_on_separators, s.read_asn_barcodes, s.ai_auto_confidence, s.ai_new_confidence, s.ai_max_new_tags, s.ai_new_types, m.role, (SELECT count(*) FROM space_members x WHERE x.space_id=s.id),
	s.created_at`

func scanSpace(row pgx.Row) (*Space, error) {
	var sp Space
	err := row.Scan(&sp.ID, &sp.Name, &sp.Kind, &sp.Description, &sp.Color, &sp.AIPolicy, &sp.AIApplyMode, &sp.AINewTags,
		&sp.DefaultLanguage, &sp.SplitOnSeparators, &sp.ReadASNBarcodes, &sp.AIAutoConfidence, &sp.AINewConfidence, &sp.AIMaxNewTags, &sp.AINewTypes, &sp.Role, &sp.MemberCount, &sp.CreatedAt)
	return &sp, err
}

// Totals are a space's documents outside the trash: how many, and their size.
type Totals struct {
	Documents int
	Bytes     int64
	at        time.Time
}

// totalsTTL bounds how old a cached count gets when a change didn't come through
// ForgetTotals (processing splitting a scan, an import).
const totalsTTL = 15 * time.Second

// SpaceTotals counts each space's documents. Counting reads every document of a space (a
// tenth of a second at 200,000) and the sidebar asks with every page, so counts are kept until
// ForgetTotals says a space changed, or for totalsTTL.
func (s *Service) SpaceTotals(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Totals, error) {
	out := make(map[uuid.UUID]Totals, len(ids))
	var missing []uuid.UUID
	s.countM.Lock()
	gen := s.forgot
	for _, id := range ids {
		if t, ok := s.counts[id]; ok && time.Since(t.at) < totalsTTL {
			out[id] = t
		} else {
			missing = append(missing, id)
		}
	}
	s.countM.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	at := time.Now()
	rows, err := s.pool.Query(ctx, `SELECT sp.id, count(d.id), coalesce(sum(d.size_bytes), 0) FROM unnest($1::uuid[]) AS sp(id)
		LEFT JOIN documents d ON d.space_id = sp.id AND d.deleted_at IS NULL GROUP BY sp.id`, missing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fresh := map[uuid.UUID]Totals{}
	for rows.Next() {
		var id uuid.UUID
		t := Totals{at: at}
		if err := rows.Scan(&id, &t.Documents, &t.Bytes); err != nil {
			return nil, err
		}
		fresh[id], out[id] = t, t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.countM.Lock()
	// A change forgotten while counting may be missing from these counts: keep them out of the cache.
	if s.forgot == gen {
		if s.counts == nil || len(s.counts) > 10000 {
			s.counts = map[uuid.UUID]Totals{}
		}
		maps.Copy(s.counts, fresh)
	}
	s.countM.Unlock()
	return out, nil
}

// ForgetTotals drops the cached counts of spaces whose documents were added, trashed,
// restored or moved; with no spaces, of every space.
func (s *Service) ForgetTotals(ids ...uuid.UUID) {
	s.countM.Lock()
	s.forgot++
	if len(ids) == 0 {
		s.counts = nil
	}
	for _, id := range ids {
		delete(s.counts, id)
	}
	s.countM.Unlock()
}

func (s *Service) fillCounts(ctx context.Context, list []*Space) error {
	ids := make([]uuid.UUID, len(list))
	for i, sp := range list {
		ids[i] = sp.ID
	}
	totals, err := s.SpaceTotals(ctx, ids)
	if err != nil {
		return err
	}
	for _, sp := range list {
		sp.DocumentCount = totals[sp.ID].Documents
	}
	return nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.fillCounts(ctx, out)
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
	if err != nil {
		return nil, err
	}
	return sp, s.fillCounts(ctx, []*Space{sp})
}

type Input struct {
	Name            *string `json:"name"`
	Description     *string `json:"description"`
	Color           *string `json:"color"`
	AIPolicy        *string `json:"ai_policy"`
	AIApplyMode     *string `json:"ai_apply_mode"`
	AINewTags       *bool   `json:"ai_new_tags"`
	DefaultLanguage *string `json:"default_language"`

	SplitOnSeparators *bool `json:"split_on_separators"`
	ReadASNBarcodes   *bool `json:"read_asn_barcodes"`

	AIAutoConfidence *int  `json:"ai_auto_confidence"`
	AINewConfidence  *int  `json:"ai_new_confidence"`
	AIMaxNewTags     *int  `json:"ai_max_new_tags"`
	AINewTypes       *bool `json:"ai_new_types"`
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
	if in.AIAutoConfidence != nil && (*in.AIAutoConfidence < 50 || *in.AIAutoConfidence > 100) {
		v.Add("ai_auto_confidence", "Between 50 and 100 percent")
	}
	if in.AINewConfidence != nil && (*in.AINewConfidence < 0 || *in.AINewConfidence > 100) {
		v.Add("ai_new_confidence", "Between 0 and 100 percent")
	}
	if in.AIMaxNewTags != nil && (*in.AIMaxNewTags < 1 || *in.AIMaxNewTags > 10) {
		v.Add("ai_max_new_tags", "Between 1 and 10")
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
		if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, description, color, ai_policy, ai_apply_mode, default_language, created_by, ai_new_tags)
			VALUES ($1,$2,'shared',coalesce($3,''),coalesce($4,'indigo'),coalesce($5,'off'),coalesce($6,'suggest'),coalesce($7,'en'),$8,coalesce($9,true))`,
			id, *in.Name, in.Description, in.Color, in.AIPolicy, in.AIApplyMode, in.DefaultLanguage, p.UserID, in.AINewTags); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,'owner')`, id, p.UserID); err != nil {
			return err
		}
		return tuneAI(ctx, tx, id, in)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// tuneAI saves the AI fine-tuning fields that were given.
func tuneAI(ctx context.Context, q db.Querier, id uuid.UUID, in Input) error {
	_, err := q.Exec(ctx, `UPDATE spaces SET ai_auto_confidence=coalesce($2,ai_auto_confidence), ai_new_confidence=coalesce($3,ai_new_confidence),
		ai_max_new_tags=coalesce($4,ai_max_new_tags), ai_new_types=coalesce($5,ai_new_types) WHERE id=$1`,
		id, in.AIAutoConfidence, in.AINewConfidence, in.AIMaxNewTags, in.AINewTypes)
	return err
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
		split_on_separators=coalesce($8,split_on_separators), read_asn_barcodes=coalesce($9,read_asn_barcodes),
		ai_new_tags=coalesce($10,ai_new_tags)
		WHERE id=$1`, id, in.Name, in.Description, in.Color, in.AIPolicy, in.AIApplyMode, in.DefaultLanguage, in.SplitOnSeparators, in.ReadASNBarcodes, in.AINewTags)
	if err == nil {
		err = tuneAI(ctx, s.pool, id, in)
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p, id)
}

// Delete removes an empty shared space. Spaces with documents must be emptied first
// (documents moved or deleted) so nothing is lost by accident.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if _, err := s.Require(ctx, p, id, ActManage); err != nil {
		return err
	}
	sp, err := s.Get(ctx, p, id)
	if err != nil {
		return err
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
	if r, err := s.Require(ctx, p, spaceID, ActManage); err != nil {
		if r != "" && p.Has(auth.ScopeWrite) {
			return apperr.Forbidden("Only space owners can manage members")
		}
		return err
	}
	sp, err := s.Get(ctx, p, spaceID)
	if err != nil {
		return err
	}
	if !role.Valid() {
		return apperr.Invalid("role", "Must be owner, editor or viewer")
	}
	if sp.Kind == "personal" && userID != p.UserID {
		return apperr.Conflict("personal_space", "Personal spaces can't be shared. Create a shared space instead, or share individual documents.")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockSpace(ctx, tx, spaceID); err != nil {
			return err
		}
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
	// Changing who is in a space needs a token that may change things, even to leave it.
	if !p.Has(auth.ScopeWrite) {
		return apperr.Forbidden("This token doesn't have the required scope")
	}
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
		if err := lockSpace(ctx, tx, spaceID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM space_members WHERE space_id=$1 AND user_id=$2`, spaceID, userID); err != nil {
			return err
		}
		return ensureOwner(ctx, tx, spaceID)
	})
}

// lockSpace makes membership changes to one space take turns, so the owner check after a
// change sees the others: two owners demoting each other at the same moment would otherwise
// both pass it and leave the space without an owner.
func lockSpace(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM spaces WHERE id=$1 FOR UPDATE`, spaceID)
	return err
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
