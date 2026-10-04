// Package identity implements users, sessions, API tokens and OIDC login (DESIGN §15).
package identity

import (
	"context"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/ratelimit"
	"github.com/anand34577/docveta/internal/platform/settings"
	"github.com/anand34577/docveta/internal/spaces"
)

type User struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	IsAdmin     bool       `json:"is_admin"`
	Status      string     `json:"status"`
	Locale      string     `json:"locale"`
	Timezone    string     `json:"timezone"`
	DateFormat  string     `json:"date_format"`
	Theme       string     `json:"theme"`
	HasPassword bool       `json:"has_password"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Event is emitted for notifications (new login, token created...). The identity
// package doesn't know about notification delivery.
type Event struct {
	Type   string
	UserID uuid.UUID
	Title  string
	Body   string
}

type Service struct {
	pool     *pgxpool.Pool
	cfg      *config.Config
	keys     *crypto.Keys
	settings *settings.Store
	audit    *audit.Log
	log      *slog.Logger

	loginLimiter   *ratelimit.Limiter // per IP+email
	loginIPLimiter *ratelimit.Limiter // per IP
	totpLimiter    *ratelimit.Limiter // second-factor attempts per user
	inviteLimiter  *ratelimit.Limiter // invitation lookups per IP

	// OnEvent is called after security events (set by the notify wiring).
	OnEvent func(ctx context.Context, e Event)
}

func NewService(pool *pgxpool.Pool, cfg *config.Config, keys *crypto.Keys, st *settings.Store, al *audit.Log, log *slog.Logger) *Service {
	return &Service{
		pool: pool, cfg: cfg, keys: keys, settings: st, audit: al, log: log,
		loginLimiter:   ratelimit.New(10, 15*time.Minute),
		loginIPLimiter: ratelimit.New(60, 15*time.Minute),
		totpLimiter:    ratelimit.New(8, 5*time.Minute),
		inviteLimiter:  ratelimit.New(60, 15*time.Minute),
	}
}

func (s *Service) emit(ctx context.Context, e Event) {
	if s.OnEvent != nil {
		s.OnEvent(ctx, e)
	}
}

const userCols = `id, email, display_name, is_admin, status, locale, timezone, date_format, theme,
	password_hash IS NOT NULL, last_login_at, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsAdmin, &u.Status, &u.Locale, &u.Timezone, &u.DateFormat,
		&u.Theme, &u.HasPassword, &u.LastLoginAt, &u.CreatedAt)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("User")
	}
	return &u, err
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

// SetupNeeded reports whether the instance has no users yet (first-run wizard).
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return !exists, err
}

type NewUser struct {
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	Password    *string `json:"password"`
	IsAdmin     bool    `json:"is_admin"`
}

func normalizeEmail(e string) (string, bool) {
	e = strings.TrimSpace(e)
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || len(e) > 254 {
		return e, false
	}
	return strings.ToLower(e), true
}

func validatePassword(v *apperr.Validation, field, pw string) {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < 10:
		v.Add(field, "Use at least 10 characters")
	case n > 256:
		v.Add(field, "Password is too long")
	}
}

func (in *NewUser) validate() error {
	var v apperr.Validation
	email, ok := normalizeEmail(in.Email)
	if !ok {
		v.Add("email", "Enter a valid email address")
	}
	in.Email = email
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" || utf8.RuneCountInString(in.DisplayName) > 80 {
		v.Add("display_name", "Name must be 1–80 characters")
	}
	if in.Password != nil {
		validatePassword(&v, "password", *in.Password)
	}
	return v.Err()
}

// createUser inserts a user and their personal space inside tx.
func (s *Service) createUser(ctx context.Context, tx pgx.Tx, in NewUser) (uuid.UUID, error) {
	var hash *string
	if in.Password != nil {
		h, err := crypto.HashPassword(*in.Password)
		if err != nil {
			return uuid.Nil, err
		}
		hash = &h
	}
	id := uuid.Must(uuid.NewV7())
	_, err := tx.Exec(ctx, `INSERT INTO users (id, email, display_name, password_hash, is_admin) VALUES ($1,$2,$3,$4,$5)`,
		id, in.Email, in.DisplayName, hash, in.IsAdmin)
	if db.IsUniqueViolation(err) {
		return uuid.Nil, apperr.Conflict("email_taken", "A user with this email already exists")
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := spaces.CreatePersonal(ctx, tx, id, in.DisplayName); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

type SetupInput struct {
	NewUser
	SharedSpaceName string `json:"shared_space_name"` // optional, e.g. "Family" or "Company"
}

// Setup creates the first administrator. It is only allowed while no users exist; the
// check and insert happen under a table lock so two concurrent setups can't both win.
func (s *Service) Setup(ctx context.Context, in SetupInput) (*User, error) {
	if in.Password == nil {
		return nil, apperr.Invalid("password", "Password is required")
	}
	in.IsAdmin = true
	if err := in.validate(); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return apperr.Conflict("already_setup", "Docveta is already set up. Please sign in.")
		}
		var err error
		id, err = s.createUser(ctx, tx, in.NewUser)
		if err != nil {
			return err
		}
		if name := strings.TrimSpace(in.SharedSpaceName); name != "" {
			sid := uuid.Must(uuid.NewV7())
			if _, err := tx.Exec(ctx, `INSERT INTO spaces (id, name, kind, created_by) VALUES ($1,$2,'shared',$3)`, sid, name, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role) VALUES ($1,$2,'owner')`, sid, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "instance.setup", "user", id.String(), nil)
	return s.GetUser(ctx, id)
}

// CreateUser is an admin action.
func (s *Service) CreateUser(ctx context.Context, p *auth.Principal, in NewUser) (*User, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("Only administrators can add users")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		id, err = s.createUser(ctx, tx, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "user.create", "user", id.String(), map[string]any{"email": in.Email, "admin": in.IsAdmin})
	return s.GetUser(ctx, id)
}

// ListUsers returns all users (admin view).
func (s *Service) ListUsers(ctx context.Context, p *auth.Principal) ([]*User, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY lower(display_name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DirectoryEntry is the minimal public info about a user, for member pickers and mentions.
type DirectoryEntry struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

func (s *Service) Directory(ctx context.Context, p *auth.Principal) ([]DirectoryEntry, error) {
	if p == nil || p.UserID == uuid.Nil {
		return nil, apperr.Unauthorized("")
	}
	rows, err := s.pool.Query(ctx, `SELECT id, email, display_name FROM users WHERE status='active' ORDER BY lower(display_name)`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DirectoryEntry, error) {
		var d DirectoryEntry
		err := r.Scan(&d.ID, &d.Email, &d.DisplayName)
		return d, err
	})
}

type AdminUserUpdate struct {
	DisplayName *string `json:"display_name"`
	IsAdmin     *bool   `json:"is_admin"`
	Status      *string `json:"status"`
	Password    *string `json:"password"` // admin reset
}

func (s *Service) AdminUpdateUser(ctx context.Context, p *auth.Principal, id uuid.UUID, in AdminUserUpdate) (*User, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	var v apperr.Validation
	if in.DisplayName != nil {
		n := strings.TrimSpace(*in.DisplayName)
		in.DisplayName = &n
		if n == "" || utf8.RuneCountInString(n) > 80 {
			v.Add("display_name", "Name must be 1–80 characters")
		}
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "disabled" {
		v.Add("status", "Must be active or disabled")
	}
	if in.Password != nil {
		validatePassword(&v, "password", *in.Password)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if id == p.UserID && ((in.IsAdmin != nil && !*in.IsAdmin) || (in.Status != nil && *in.Status == "disabled")) {
		return nil, apperr.Conflict("self_lockout", "You can't remove your own admin rights or disable yourself")
	}
	var hash *string
	if in.Password != nil {
		h, err := crypto.HashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		hash = &h
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET display_name=coalesce($2,display_name), is_admin=coalesce($3,is_admin),
			status=coalesce($4,status), password_hash=coalesce($5,password_hash), updated_at=now() WHERE id=$1`,
			id, in.DisplayName, in.IsAdmin, in.Status, hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("User")
		}
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_admin AND status='active'`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return apperr.Conflict("last_admin", "At least one active administrator is required")
		}
		if (in.Status != nil && *in.Status == "disabled") || hash != nil {
			return revokeAllFor(ctx, tx, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "user.update", "user", id.String(), map[string]any{
		"is_admin": in.IsAdmin, "status": in.Status, "password_reset": in.Password != nil})
	return s.GetUser(ctx, id)
}

// DeleteUser removes a user whose personal space is empty. Shared spaces keep their
// documents (ownership of documents becomes NULL = "former member").
func (s *Service) DeleteUser(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	if id == p.UserID {
		return apperr.Conflict("self_delete", "You can't delete your own account here")
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var docs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM documents d JOIN spaces s ON s.id=d.space_id
			JOIN space_members m ON m.space_id=s.id AND m.user_id=$1 WHERE s.kind='personal'`, id).Scan(&docs); err != nil {
			return err
		}
		if docs > 0 {
			return apperr.Conflict("personal_space_not_empty",
				"This user's personal space still has documents. Move them to another space or disable the user instead.")
		}
		// Shared spaces where they are the only owner would become ownerless.
		var orphan string
		err := tx.QueryRow(ctx, `SELECT s.name FROM spaces s JOIN space_members m ON m.space_id=s.id AND m.user_id=$1 AND m.role='owner'
			WHERE s.kind='shared' AND NOT EXISTS (SELECT 1 FROM space_members o WHERE o.space_id=s.id AND o.role='owner' AND o.user_id<>$1)
			LIMIT 1`, id).Scan(&orphan)
		if err == nil {
			return apperr.Conflict("last_owner", "This user is the only owner of \""+orphan+"\". Add another owner first.")
		} else if !db.IsNoRows(err) {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM spaces WHERE kind='personal' AND id IN
			(SELECT space_id FROM space_members WHERE user_id=$1)`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("User")
		}
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_admin AND status='active'`).Scan(&admins); err != nil {
			return err
		}
		if admins == 0 {
			return apperr.Conflict("last_admin", "At least one active administrator is required")
		}
		return nil
	})
	if err == nil {
		s.audit.Record(ctx, nil, "user.delete", "user", id.String(), nil)
	}
	return err
}

type ProfileUpdate struct {
	DisplayName *string `json:"display_name"`
	Locale      *string `json:"locale"`
	Timezone    *string `json:"timezone"`
	DateFormat  *string `json:"date_format"`
	Theme       *string `json:"theme"`
}

func (s *Service) UpdateProfile(ctx context.Context, p *auth.Principal, in ProfileUpdate) (*User, error) {
	var v apperr.Validation
	if in.DisplayName != nil {
		n := strings.TrimSpace(*in.DisplayName)
		in.DisplayName = &n
		if n == "" || utf8.RuneCountInString(n) > 80 {
			v.Add("display_name", "Name must be 1–80 characters")
		}
	}
	if in.Timezone != nil {
		if _, err := time.LoadLocation(*in.Timezone); err != nil {
			v.Add("timezone", "Unknown time zone")
		}
	}
	if in.DateFormat != nil {
		switch *in.DateFormat {
		case "DD/MM/YYYY", "MM/DD/YYYY", "YYYY-MM-DD", "DD.MM.YYYY", "D MMM YYYY":
		default:
			v.Add("date_format", "Unsupported date format")
		}
	}
	if in.Theme != nil && *in.Theme != "system" && *in.Theme != "light" && *in.Theme != "dark" {
		v.Add("theme", "Must be system, light or dark")
	}
	if in.Locale != nil && (len(*in.Locale) < 2 || len(*in.Locale) > 16) {
		v.Add("locale", "Invalid locale")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	_, err := s.pool.Exec(ctx, `UPDATE users SET display_name=coalesce($2,display_name), locale=coalesce($3,locale),
		timezone=coalesce($4,timezone), date_format=coalesce($5,date_format), theme=coalesce($6,theme), updated_at=now()
		WHERE id=$1`, p.UserID, in.DisplayName, in.Locale, in.Timezone, in.DateFormat, in.Theme)
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, p.UserID)
}

// ChangePassword requires the current password (if one is set) and revokes other sessions.
func (s *Service) ChangePassword(ctx context.Context, p *auth.Principal, current, next string) error {
	var v apperr.Validation
	validatePassword(&v, "new_password", next)
	if err := v.Err(); err != nil {
		return err
	}
	var hash *string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	if hash != nil {
		ok, err := crypto.VerifyPassword(current, *hash)
		if err != nil || !ok {
			return apperr.Invalid("current_password", "Current password is incorrect")
		}
	}
	nh, err := crypto.HashPassword(next)
	if err != nil {
		return err
	}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, p.UserID, nh); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, p.UserID, p.SessionID)
		return err
	})
	if err == nil {
		s.audit.Record(ctx, nil, "user.password_change", "user", p.UserID.String(), nil)
	}
	return err
}

func revokeAllFor(ctx context.Context, q db.Querier, userID uuid.UUID) error {
	if _, err := q.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}
