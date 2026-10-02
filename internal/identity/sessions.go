package identity

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

const (
	sessionPrefix = "dvt_ses"
	tokenPrefix   = "dvt_pat"
)

type Session struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

var errBadCredentials = &apperr.Error{Kind: apperr.KindUnauthorized, Code: "invalid_credentials", Msg: "Email or password is incorrect"}

// PasswordLoginAllowed reports whether local password login is enabled (it can be
// turned off when OIDC is the only allowed method).
func (s *Service) PasswordLoginAllowed(ctx context.Context) bool {
	cfg, _ := s.OIDCConfig(ctx)
	return cfg == nil || !cfg.Enabled || !cfg.DisablePasswordLogin
}

// Login verifies a password and creates a session. Timing is equalized for unknown
// accounts, and attempts are rate limited per IP and per IP+email.
func (s *Service) Login(ctx context.Context, email, password, userAgent string) (token string, u *User, err error) {
	ip := httpx.ClientIP(ctx)
	email = strings.ToLower(strings.TrimSpace(email))
	if ok, wait := s.loginIPLimiter.Allow(ip); !ok {
		return "", nil, rateLimited(wait)
	}
	key := ip + "|" + email
	if ok, wait := s.loginLimiter.Allow(key); !ok {
		return "", nil, rateLimited(wait)
	}
	if !s.PasswordLoginAllowed(ctx) {
		return "", nil, apperr.Forbidden("Password sign-in is disabled. Use single sign-on.")
	}
	var id uuid.UUID
	var hash *string
	var status string
	err = s.pool.QueryRow(ctx, `SELECT id, password_hash, status FROM users WHERE email=$1`, email).Scan(&id, &hash, &status)
	if db.IsNoRows(err) || (err == nil && hash == nil) {
		crypto.DummyVerify(password)
		s.audit.Record(ctx, nil, "auth.login_failed", "user", "", map[string]any{"email": email})
		return "", nil, errBadCredentials
	}
	if err != nil {
		return "", nil, err
	}
	ok, err := crypto.VerifyPassword(password, *hash)
	if err != nil || !ok {
		s.audit.Record(ctx, nil, "auth.login_failed", "user", id.String(), map[string]any{"email": email})
		return "", nil, errBadCredentials
	}
	if status != "active" {
		return "", nil, apperr.Forbidden("This account is disabled. Ask an administrator.")
	}
	s.loginLimiter.Reset(key)
	token, err = s.CreateSession(ctx, id, userAgent, "password")
	if err != nil {
		return "", nil, err
	}
	u, err = s.GetUser(ctx, id)
	return token, u, err
}

func rateLimited(wait time.Duration) error {
	return &apperr.Error{Kind: apperr.KindRateLimited, Code: "rate_limited",
		Msg:   "Too many attempts. Please wait a few minutes and try again.",
		Extra: map[string]any{"retry_after_seconds": int(wait.Seconds()) + 1}}
}

// CreateSession issues a new session token for a user.
func (s *Service) CreateSession(ctx context.Context, userID uuid.UUID, userAgent, method string) (string, error) {
	token, _ := crypto.NewToken(sessionPrefix)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	id := uuid.Must(uuid.NewV7())
	ip := httpx.ClientIP(ctx)
	known := false
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// A device counts as known if an earlier session came from the same IP and the
		// same kind of device; the very first session isn't worth an alert either.
		rows, err := tx.Query(ctx, `SELECT ip, user_agent FROM sessions WHERE user_id=$1`, userID)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			var pIP, pUA string
			if err := rows.Scan(&pIP, &pUA); err != nil {
				return err
			}
			n++
			known = known || (pIP == ip && shortUA(pUA) == shortUA(userAgent))
		}
		if err := rows.Err(); err != nil {
			return err
		}
		known = known || n == 0
		if _, err := tx.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, user_agent, ip, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, id, userID, crypto.HashToken(token), userAgent, ip, time.Now().Add(s.cfg.SessionMax)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, userID)
		return err
	})
	if err != nil {
		return "", err
	}
	s.audit.Record(auth.With(ctx, &auth.Principal{Kind: auth.KindSession, UserID: userID}), nil, "auth.login", "session", id.String(),
		map[string]any{"method": method})
	if known {
		return token, nil
	}
	s.emit(ctx, Event{Type: "security.new_login", UserID: userID, Title: "New sign-in to your account",
		Body: "Signed in from " + ip + " (" + shortUA(userAgent) + "). If this wasn't you, change your password and sign out other sessions."})
	return token, nil
}

func shortUA(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		return "iOS"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Mac OS"):
		return "macOS"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	case ua == "":
		return "unknown device"
	}
	if len(ua) > 40 {
		return ua[:40]
	}
	return ua
}

// AuthenticateSession resolves a session token into a principal.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (*auth.Principal, error) {
	if !strings.HasPrefix(token, sessionPrefix+"_") {
		return nil, nil
	}
	var p auth.Principal
	var lastSeen time.Time
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.last_seen_at, u.id, u.is_admin, u.email, u.display_name
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now()
		  AND s.last_seen_at > now() - make_interval(secs => $2) AND u.status='active'`,
		crypto.HashToken(token), s.cfg.SessionIdle.Seconds()).Scan(&p.SessionID, &lastSeen, &p.UserID, &p.IsAdmin, &p.Email, &p.Name)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Kind = auth.KindSession
	if time.Since(lastSeen) > 5*time.Minute { // throttle writes
		_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=now(), ip=$2 WHERE id=$1`, p.SessionID, httpx.ClientIP(ctx))
	}
	return &p, nil
}

func (s *Service) Logout(ctx context.Context, p *auth.Principal) error {
	if p == nil || p.Kind != auth.KindSession {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, p.SessionID)
	return err
}

func (s *Service) ListSessions(ctx context.Context, p *auth.Principal) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_agent, ip, created_at, last_seen_at, expires_at FROM sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now() ORDER BY last_seen_at DESC`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Session, error) {
		var x Session
		err := r.Scan(&x.ID, &x.UserAgent, &x.IP, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt)
		x.Current = x.ID == p.SessionID
		return x, err
	})
}

// RevokeSession revokes one session; id == uuid.Nil revokes all other sessions.
func (s *Service) RevokeSession(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if id == uuid.Nil {
		_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, p.UserID, p.SessionID)
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, p.UserID)
	if err == nil && tag.RowsAffected() == 0 {
		return apperr.NotFound("Session")
	}
	return err
}

// CleanupSessions deletes long-expired sessions.
func (s *Service) CleanupSessions(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '7 days' OR revoked_at < now() - interval '7 days'`)
	return err
}
