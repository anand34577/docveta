package identity

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
)

type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type NewToken struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days"`
}

// CreateToken issues a personal access token. Only interactive sessions may create
// tokens (a leaked token can't mint more tokens).
func (s *Service) CreateToken(ctx context.Context, p *auth.Principal, in NewToken) (*APIToken, string, error) {
	if p == nil || p.Kind != auth.KindSession {
		return nil, "", apperr.Forbidden("API tokens can only be created from a signed-in browser session")
	}
	var v apperr.Validation
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 {
		v.Add("name", "Name must be 1–80 characters")
	}
	if len(in.Scopes) == 0 {
		v.Add("scopes", "Choose at least one permission")
	}
	for _, sc := range in.Scopes {
		if !slices.Contains(auth.AllScopes, sc) {
			v.Add("scopes", "Unknown permission %q", sc)
		}
	}
	if slices.Contains(in.Scopes, auth.ScopeAdmin) && !p.IsAdmin {
		v.Add("scopes", "Only administrators can create admin tokens")
	}
	var exp *time.Time
	if in.ExpiresInDays != nil {
		if *in.ExpiresInDays < 1 || *in.ExpiresInDays > 3650 {
			v.Add("expires_in_days", "Must be between 1 and 3650 days")
		} else {
			t := time.Now().Add(time.Duration(*in.ExpiresInDays) * 24 * time.Hour)
			exp = &t
		}
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	token, display := crypto.NewToken(tokenPrefix)
	t := &APIToken{ID: uuid.Must(uuid.NewV7()), Name: in.Name, Prefix: display, Scopes: in.Scopes, ExpiresAt: exp, CreatedAt: time.Now()}
	if _, err := s.pool.Exec(ctx, `INSERT INTO api_tokens (id, user_id, name, token_prefix, token_hash, scopes, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, t.ID, p.UserID, t.Name, t.Prefix, crypto.HashToken(token), t.Scopes, t.ExpiresAt); err != nil {
		return nil, "", err
	}
	s.audit.Record(ctx, nil, "token.create", "api_token", t.ID.String(), map[string]any{"name": t.Name, "scopes": t.Scopes})
	s.emit(ctx, Event{Type: "security.token_created", UserID: p.UserID, Title: "New API token created",
		Body: "A token named \"" + t.Name + "\" was created for your account."})
	return t, token, nil
}

func (s *Service) ListTokens(ctx context.Context, p *auth.Principal) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, token_prefix, scopes, expires_at, last_used_at, created_at
		FROM api_tokens WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (APIToken, error) {
		var t APIToken
		err := r.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scopes, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
		return t, err
	})
}

func (s *Service) RevokeToken(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, p.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Token")
	}
	s.audit.Record(ctx, nil, "token.revoke", "api_token", id.String(), nil)
	return nil
}

// AuthenticateToken resolves a personal access token.
func (s *Service) AuthenticateToken(ctx context.Context, token string) (*auth.Principal, error) {
	if !strings.HasPrefix(token, tokenPrefix+"_") {
		return nil, nil
	}
	var p auth.Principal
	var lastUsed *time.Time
	err := s.pool.QueryRow(ctx, `SELECT t.id, t.scopes, t.last_used_at, u.id, u.is_admin, u.email, u.display_name
		FROM api_tokens t JOIN users u ON u.id=t.user_id
		WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now()) AND u.status='active'`,
		crypto.HashToken(token)).Scan(&p.TokenID, &p.Scopes, &lastUsed, &p.UserID, &p.IsAdmin, &p.Email, &p.Name)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Kind = auth.KindToken
	if lastUsed == nil || time.Since(*lastUsed) > 5*time.Minute {
		_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at=now() WHERE id=$1`, p.TokenID)
	}
	return &p, nil
}
