package identity

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/spaces"
)

const inviteTokenPrefix = "dvt_inv"

// InviteSpace is a shared space (and role in it) a new member gets on joining.
type InviteSpace struct {
	SpaceID uuid.UUID   `json:"space_id"`
	Role    spaces.Role `json:"role"`
	Name    string      `json:"name,omitempty"`
}

type Invite struct {
	ID          uuid.UUID     `json:"id"`
	Email       *string       `json:"email"`
	DisplayName string        `json:"display_name"`
	IsAdmin     bool          `json:"is_admin"`
	Spaces      []InviteSpace `json:"spaces"`
	Note        string        `json:"note"`
	InvitedBy   string        `json:"invited_by"`
	CreatedAt   time.Time     `json:"created_at"`
	ExpiresAt   time.Time     `json:"expires_at"`
	AcceptedAt  *time.Time    `json:"accepted_at"`
	Status      string        `json:"status"` // pending | accepted | expired | revoked
}

type NewInvite struct {
	Email         *string       `json:"email"`
	DisplayName   string        `json:"display_name"`
	IsAdmin       bool          `json:"is_admin"`
	Spaces        []InviteSpace `json:"spaces"`
	Note          string        `json:"note"`
	ExpiresInDays *int          `json:"expires_in_days"`
}

// CreateInvite makes a single-use link. The token is returned once and only its hash is stored.
func (s *Service) CreateInvite(ctx context.Context, p *auth.Principal, in NewInvite) (*Invite, string, error) {
	if !p.Admin() {
		return nil, "", apperr.Forbidden("Only administrators can invite people")
	}
	var v apperr.Validation
	var email *string
	if in.Email != nil && strings.TrimSpace(*in.Email) != "" {
		e, ok := normalizeEmail(*in.Email)
		if !ok {
			v.Add("email", "Enter a valid email address")
		}
		email = &e
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if utf8.RuneCountInString(in.DisplayName) > 80 {
		v.Add("display_name", "Name must be at most 80 characters")
	}
	days := 7
	if in.ExpiresInDays != nil {
		days = *in.ExpiresInDays
	}
	if days < 1 || days > 90 {
		v.Add("expires_in_days", "Must be between 1 and 90 days")
	}
	seen := map[uuid.UUID]bool{}
	for i := range in.Spaces {
		sp := &in.Spaces[i]
		if !sp.Role.Valid() || seen[sp.SpaceID] {
			v.Add("spaces", "Choose a role for each space, once")
			break
		}
		seen[sp.SpaceID] = true
		err := s.pool.QueryRow(ctx, `SELECT name FROM spaces WHERE id=$1 AND kind='shared'`, sp.SpaceID).Scan(&sp.Name)
		if db.IsNoRows(err) {
			v.Add("spaces", "Invitations can only add people to shared spaces that exist")
			break
		}
		if err != nil {
			return nil, "", err
		}
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	if email != nil {
		var taken bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, *email).Scan(&taken); err != nil {
			return nil, "", err
		}
		if taken {
			return nil, "", apperr.Conflict("email_taken", "Someone with this email already has an account")
		}
	}
	spacesJSON, _ := json.Marshal(in.Spaces)
	token, _ := crypto.NewToken(inviteTokenPrefix)
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO invites (id, token_hash, email, display_name, is_admin, spaces, note, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now() + make_interval(days => $9))`,
		id, crypto.HashToken(token), email, in.DisplayName, in.IsAdmin, spacesJSON, truncateStr(strings.TrimSpace(in.Note), 500), p.UserID, days); err != nil {
		return nil, "", err
	}
	s.audit.Record(ctx, nil, "invite.create", "invite", id.String(), map[string]any{"email": email, "admin": in.IsAdmin})
	inv, err := s.getInvite(ctx, id)
	return inv, token, err
}

func truncateStr(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

const inviteSelect = `SELECT i.id, i.email, i.display_name, i.is_admin, i.spaces, i.note, coalesce(u.display_name, ''), i.created_at,
	i.expires_at, i.accepted_at, CASE WHEN i.revoked_at IS NOT NULL THEN 'revoked' WHEN i.accepted_at IS NOT NULL THEN 'accepted'
	WHEN i.expires_at < now() THEN 'expired' ELSE 'pending' END
	FROM invites i LEFT JOIN users u ON u.id=i.created_by`

func scanInvite(row pgx.Row) (*Invite, error) {
	var i Invite
	var spacesJSON []byte
	var email *string
	if err := row.Scan(&i.ID, &email, &i.DisplayName, &i.IsAdmin, &spacesJSON, &i.Note, &i.InvitedBy, &i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.Status); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Invitation")
		}
		return nil, err
	}
	i.Email = email
	_ = json.Unmarshal(spacesJSON, &i.Spaces)
	if i.Spaces == nil {
		i.Spaces = []InviteSpace{}
	}
	return &i, nil
}

func (s *Service) getInvite(ctx context.Context, id uuid.UUID) (*Invite, error) {
	return scanInvite(s.pool.QueryRow(ctx, inviteSelect+` WHERE i.id=$1`, id))
}

func (s *Service) ListInvites(ctx context.Context, p *auth.Principal) ([]*Invite, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	rows, err := s.pool.Query(ctx, inviteSelect+` ORDER BY i.created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Service) RevokeInvite(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE invites SET revoked_at=now() WHERE id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Pending invitation")
	}
	s.audit.Record(ctx, nil, "invite.revoke", "invite", id.String(), nil)
	return nil
}

// InvitePreview is what the (signed-out) invitee sees before choosing a password.
type InvitePreview struct {
	Email         *string   `json:"email"`
	DisplayName   string    `json:"display_name"`
	InvitedBy     string    `json:"invited_by"`
	ExpiresAt     time.Time `json:"expires_at"`
	PasswordLogin bool      `json:"password_login"`
}

var errBadInvite = &apperr.Error{Kind: apperr.KindNotFound, Code: "invalid_invite",
	Msg: "This invitation link isn't valid any more. Ask the person who invited you for a new one."}

func (s *Service) inviteLimited(ctx context.Context) error {
	if ok, wait := s.inviteLimiter.Allow(httpx.ClientIP(ctx)); !ok {
		return rateLimited(wait)
	}
	return nil
}

// InvitePreviewFor looks a token up. Unknown, used, revoked and expired links all look the same.
func (s *Service) InvitePreviewFor(ctx context.Context, token string) (*InvitePreview, error) {
	if err := s.inviteLimited(ctx); err != nil {
		return nil, err
	}
	inv, err := scanInvite(s.pool.QueryRow(ctx, inviteSelect+` WHERE i.token_hash=$1`, crypto.HashToken(token)))
	if err != nil || inv.Status != "pending" {
		return nil, errBadInvite
	}
	return &InvitePreview{Email: inv.Email, DisplayName: inv.DisplayName, InvitedBy: inv.InvitedBy, ExpiresAt: inv.ExpiresAt,
		PasswordLogin: s.PasswordLoginAllowed(ctx)}, nil
}

type AcceptInvite struct {
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Password    *string `json:"password"`
}

// AcceptInvitation creates the account, joins the invited spaces and signs the new
// member in. The link works once.
func (s *Service) AcceptInvitation(ctx context.Context, token string, in AcceptInvite, userAgent string) (string, *User, error) {
	if err := s.inviteLimited(ctx); err != nil {
		return "", nil, err
	}
	var userID uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		var email *string
		var name string
		var admin bool
		var spacesJSON []byte
		err := tx.QueryRow(ctx, `SELECT id, email, display_name, is_admin, spaces FROM invites
			WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now() FOR UPDATE`,
			crypto.HashToken(token)).Scan(&id, &email, &name, &admin, &spacesJSON)
		if db.IsNoRows(err) {
			return errBadInvite
		}
		if err != nil {
			return err
		}
		nu := NewUser{Email: in.Email, DisplayName: strings.TrimSpace(in.DisplayName), Password: in.Password, IsAdmin: admin}
		if email != nil {
			nu.Email = *email // the invitation decides the address
		}
		if nu.DisplayName == "" {
			nu.DisplayName = name
		}
		if nu.Password == nil && s.PasswordLoginAllowed(ctx) {
			return apperr.Invalid("password", "Choose a password")
		}
		if err := nu.validate(); err != nil {
			return err
		}
		if userID, err = s.createUser(ctx, tx, nu); err != nil {
			return err
		}
		var spacesList []InviteSpace
		_ = json.Unmarshal(spacesJSON, &spacesList)
		for _, sp := range spacesList {
			if _, err := tx.Exec(ctx, `INSERT INTO space_members (space_id, user_id, role)
				SELECT id, $2, $3 FROM spaces WHERE id=$1 ON CONFLICT DO NOTHING`, sp.SpaceID, userID, string(sp.Role)); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE invites SET accepted_at=now(), accepted_by=$2 WHERE id=$1`, id, userID)
		return err
	})
	if err != nil {
		return "", nil, err
	}
	s.audit.Record(auth.With(ctx, &auth.Principal{Kind: auth.KindSession, UserID: userID}), nil, "invite.accept", "user", userID.String(), nil)
	sess, err := s.CreateSession(ctx, userID, userAgent, "invite")
	if err != nil {
		return "", nil, err
	}
	u, err := s.GetUser(ctx, userID)
	return sess, u, err
}
