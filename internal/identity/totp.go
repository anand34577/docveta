package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP (RFC 6238) is defined over HMAC-SHA1, which authenticator apps expect
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
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
	totpStep   = 30 * time.Second
	totpDigits = 6
	recoveryN  = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// hotp is RFC 4226: HMAC-SHA1 over the counter, dynamically truncated.
func hotp(secret []byte, counter int64) string {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(counter))
	m := hmac.New(sha1.New, secret)
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) | (uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000)
}

// verifyTOTP accepts the code for the current 30-second step or the one either side
// (clock drift), but never a step at or before lastCounter: a code works once.
func verifyTOTP(secret []byte, code string, now time.Time, lastCounter int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / int64(totpStep.Seconds())
	for _, c := range []int64{cur, cur - 1, cur + 1} {
		if c > lastCounter && hmac.Equal([]byte(hotp(secret, c)), []byte(code)) {
			return c, true
		}
	}
	return 0, false
}

// TwoFactorStatus is what the Security settings page shows.
type TwoFactorStatus struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
}

func (s *Service) TwoFactorStatus(ctx context.Context, userID uuid.UUID) (*TwoFactorStatus, error) {
	st := &TwoFactorStatus{}
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_totp WHERE user_id=$1 AND confirmed_at IS NOT NULL),
		(SELECT count(*) FROM recovery_codes WHERE user_id=$1 AND used_at IS NULL)`, userID).Scan(&st.Enabled, &st.RecoveryCodesLeft)
	if !st.Enabled {
		st.RecoveryCodesLeft = 0
	}
	return st, err
}

func (s *Service) twoFactorEnabled(ctx context.Context, userID uuid.UUID) bool {
	var on bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_totp WHERE user_id=$1 AND confirmed_at IS NOT NULL)`, userID).Scan(&on)
	return on
}

// TwoFactorSetup starts enrolment: a fresh secret the user adds to an authenticator app.
// Nothing changes for sign-in until they confirm with a code (TwoFactorEnable).
func (s *Service) TwoFactorSetup(ctx context.Context, p *auth.Principal) (secret, uri string, err error) {
	if p == nil || p.Kind != auth.KindSession {
		return "", "", apperr.Forbidden("Two-factor sign-in is set up from a signed-in browser session")
	}
	if s.twoFactorEnabled(ctx, p.UserID) {
		return "", "", apperr.Conflict("already_enabled", "Two-factor sign-in is already on. Turn it off first to set it up again.")
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO user_totp (user_id, secret_enc) VALUES ($1,$2)
		ON CONFLICT (user_id) DO UPDATE SET secret_enc=excluded.secret_enc, confirmed_at=NULL, last_counter=0, created_at=now()`,
		p.UserID, s.keys.Encrypt(raw)); err != nil {
		return "", "", err
	}
	secret = b32.EncodeToString(raw)
	label := url.PathEscape("Docveta:" + p.Email)
	uri = fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=Docveta&algorithm=SHA1&digits=%d&period=%d", label, secret, totpDigits, int(totpStep.Seconds()))
	return secret, uri, nil
}

func (s *Service) loadSecret(ctx context.Context, q db.Querier, userID uuid.UUID, forUpdate bool) (secret []byte, last int64, confirmed bool, err error) {
	sql := `SELECT secret_enc, last_counter, confirmed_at IS NOT NULL FROM user_totp WHERE user_id=$1`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	var sealed []byte
	if err = q.QueryRow(ctx, sql, userID).Scan(&sealed, &last, &confirmed); err != nil {
		return nil, 0, false, err
	}
	secret, err = s.keys.Decrypt(sealed)
	return secret, last, confirmed, err
}

// TwoFactorEnable confirms enrolment with a code from the app and returns the one-time
// recovery codes (shown once; only their hashes are stored).
func (s *Service) TwoFactorEnable(ctx context.Context, p *auth.Principal, code string) ([]string, error) {
	if p == nil || p.Kind != auth.KindSession {
		return nil, apperr.Forbidden("")
	}
	if ok, wait := s.totpLimiter.Allow(p.UserID.String()); !ok {
		return nil, rateLimited(wait)
	}
	var codes []string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		secret, last, confirmed, err := s.loadSecret(ctx, tx, p.UserID, true)
		if db.IsNoRows(err) {
			return apperr.Conflict("not_started", "Start setup first")
		}
		if err != nil {
			return err
		}
		if confirmed {
			return apperr.Conflict("already_enabled", "Two-factor sign-in is already on")
		}
		counter, ok := verifyTOTP(secret, code, time.Now(), last)
		if !ok {
			return apperr.Invalid("code", "That code isn't right. Check the time on your phone and try the next code.")
		}
		if _, err := tx.Exec(ctx, `UPDATE user_totp SET confirmed_at=now(), last_counter=$2 WHERE user_id=$1`, p.UserID, counter); err != nil {
			return err
		}
		codes, err = s.newRecoveryCodes(ctx, tx, p.UserID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.totpLimiter.Reset(p.UserID.String())
	s.audit.Record(ctx, nil, "auth.2fa_enabled", "user", p.UserID.String(), nil)
	s.emit(ctx, Event{Type: "security.2fa_changed", UserID: p.UserID, Title: "Two-factor sign-in turned on",
		Body: "Your account now asks for a code from your authenticator app at sign-in."})
	return codes, nil
}

func (s *Service) newRecoveryCodes(ctx context.Context, q db.Querier, userID uuid.UUID) ([]string, error) {
	if _, err := q.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, recoveryN)
	for i := range codes {
		raw := make([]byte, 5)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		c := strings.ToLower(b32.EncodeToString(raw)) // 8 characters
		codes[i] = c[:4] + "-" + c[4:]
		if _, err := q.Exec(ctx, `INSERT INTO recovery_codes (id, user_id, code_hash) VALUES ($1,$2,$3)`,
			uuid.Must(uuid.NewV7()), userID, crypto.HashToken(normalizeRecovery(codes[i]))); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

func normalizeRecovery(c string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

// checkSecondFactor verifies a TOTP code or a recovery code (which it consumes).
func (s *Service) checkSecondFactor(ctx context.Context, tx pgx.Tx, userID uuid.UUID, code string) (bool, error) {
	secret, last, confirmed, err := s.loadSecret(ctx, tx, userID, true)
	if db.IsNoRows(err) || (err == nil && !confirmed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if counter, ok := verifyTOTP(secret, code, time.Now(), last); ok {
		_, err := tx.Exec(ctx, `UPDATE user_totp SET last_counter=$2 WHERE user_id=$1`, userID, counter)
		return err == nil, err
	}
	tag, err := tx.Exec(ctx, `UPDATE recovery_codes SET used_at=now() WHERE id = (
		SELECT id FROM recovery_codes WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL LIMIT 1)`,
		userID, crypto.HashToken(normalizeRecovery(code)))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// TwoFactorDisable turns it off. Requires the account password (when it has one) and a
// current code, so a stolen session alone can't remove the second factor.
func (s *Service) TwoFactorDisable(ctx context.Context, p *auth.Principal, password, code string) error {
	if p == nil || p.Kind != auth.KindSession {
		return apperr.Forbidden("")
	}
	if ok, wait := s.totpLimiter.Allow(p.UserID.String()); !ok {
		return rateLimited(wait)
	}
	var hash *string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		return err
	}
	if hash != nil {
		if ok, err := crypto.VerifyPassword(password, *hash); err != nil || !ok {
			return apperr.Invalid("password", "Password is incorrect")
		}
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		ok, err := s.checkSecondFactor(ctx, tx, p.UserID, code)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Invalid("code", "That code isn't right")
		}
		_, err = tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id=$1`, p.UserID)
		return err
	})
	if err != nil {
		return err
	}
	s.totpLimiter.Reset(p.UserID.String())
	s.audit.Record(ctx, nil, "auth.2fa_disabled", "user", p.UserID.String(), nil)
	s.emit(ctx, Event{Type: "security.2fa_changed", UserID: p.UserID, Title: "Two-factor sign-in turned off",
		Body: "If this wasn't you, change your password and sign out other sessions."})
	return nil
}

// RegenerateRecoveryCodes replaces the recovery codes (after confirming with a code).
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, p *auth.Principal, code string) ([]string, error) {
	if p == nil || p.Kind != auth.KindSession {
		return nil, apperr.Forbidden("")
	}
	if ok, wait := s.totpLimiter.Allow(p.UserID.String()); !ok {
		return nil, rateLimited(wait)
	}
	var codes []string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		ok, err := s.checkSecondFactor(ctx, tx, p.UserID, code)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.Invalid("code", "That code isn't right")
		}
		codes, err = s.newRecoveryCodes(ctx, tx, p.UserID)
		return err
	})
	if err == nil {
		s.totpLimiter.Reset(p.UserID.String())
		s.audit.Record(ctx, nil, "auth.2fa_recovery_regenerated", "user", p.UserID.String(), nil)
	}
	return codes, err
}

// AdminResetTwoFactor removes a user's second factor (lost phone and recovery codes).
func (s *Service) AdminResetTwoFactor(ctx context.Context, p *auth.Principal, userID uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM user_totp WHERE user_id=$1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("Two-factor sign-in for this user")
	}
	s.audit.Record(ctx, nil, "auth.2fa_reset", "user", userID.String(), nil)
	s.emit(ctx, Event{Type: "security.2fa_changed", UserID: userID, Title: "Two-factor sign-in was reset",
		Body: "An administrator turned off two-factor sign-in on your account. You can set it up again in Settings."})
	return nil
}

// ---------------------------------------------------------------------------
// Sign-in second step
// ---------------------------------------------------------------------------

const challengeTTL = 5 * time.Minute

func (s *Service) newChallenge(userID uuid.UUID) string {
	return userID.String() + "~" + s.keys.SignedValue("2fa-challenge", userID.String(), time.Now().Add(challengeTTL))
}

// LoginResult is either a session (Token, User) or, when the account needs a second
// factor, a short-lived Challenge to complete with LoginTwoFactor.
type LoginResult struct {
	Token     string
	User      *User
	Challenge string
}

// LoginTwoFactor finishes a sign-in that needed a second factor.
func (s *Service) LoginTwoFactor(ctx context.Context, challenge, code, userAgent string) (string, *User, error) {
	uidStr, sig, ok := strings.Cut(challenge, "~")
	uid, perr := uuid.Parse(uidStr)
	if !ok || perr != nil || !s.keys.VerifySignedValue("2fa-challenge", uid.String(), sig, time.Now()) {
		return "", nil, &apperr.Error{Kind: apperr.KindUnauthorized, Code: "challenge_expired", Msg: "That sign-in took too long. Please sign in again."}
	}
	ip := httpx.ClientIP(ctx)
	if ok, wait := s.totpLimiter.Allow(uid.String()); !ok {
		return "", nil, rateLimited(wait)
	}
	if ok, wait := s.loginIPLimiter.Allow(ip); !ok {
		return "", nil, rateLimited(wait)
	}
	var good bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, uid).Scan(&status); err != nil {
			return err
		}
		if status != "active" {
			return apperr.Forbidden("This account is disabled. Ask an administrator.")
		}
		var err error
		good, err = s.checkSecondFactor(ctx, tx, uid, code)
		return err
	})
	if err != nil {
		return "", nil, err
	}
	if !good {
		s.audit.Record(ctx, nil, "auth.2fa_failed", "user", uid.String(), nil)
		return "", nil, &apperr.Error{Kind: apperr.KindUnauthorized, Code: "invalid_code", Msg: "That code isn't right. Try the current code from your app, or a recovery code."}
	}
	s.totpLimiter.Reset(uid.String())
	token, err := s.CreateSession(ctx, uid, userAgent, "password+2fa")
	if err != nil {
		return "", nil, err
	}
	u, err := s.GetUser(ctx, uid)
	return token, u, err
}
