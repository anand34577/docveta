package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
)

const (
	oidcSettingsKey = "auth.oidc"
	oidcSecretKey   = "auth.oidc.client_secret"
)

// OIDCCallbackPath is where the provider sends people back; register <address><path> there.
const OIDCCallbackPath = "/api/v1/auth/oidc/callback"

// OIDCConfig is editable in Admin → Authentication.
type OIDCConfig struct {
	Enabled              bool     `json:"enabled"`
	Issuer               string   `json:"issuer"`
	ClientID             string   `json:"client_id"`
	Scopes               []string `json:"scopes"`
	ButtonLabel          string   `json:"button_label"`
	AutoProvision        bool     `json:"auto_provision"`
	AllowedGroups        []string `json:"allowed_groups"`
	AdminGroups          []string `json:"admin_groups"`
	GroupsClaim          string   `json:"groups_claim"`
	LinkByVerifiedEmail  bool     `json:"link_by_verified_email"`
	DisablePasswordLogin bool     `json:"disable_password_login"`
	HasClientSecret      bool     `json:"has_client_secret"`
	// ClientSecret is write-only: accepted on update, never returned.
	ClientSecret *string `json:"client_secret,omitempty"`
	// RedirectURI is read-only: the callback address to register with the provider, as seen
	// from the administrator's browser. Filled in by the API.
	RedirectURI string `json:"redirect_uri,omitempty"`
}

func (s *Service) OIDCConfig(ctx context.Context) (*OIDCConfig, error) {
	var c OIDCConfig
	found, err := s.settings.Get(ctx, oidcSettingsKey, &c)
	if err != nil || !found {
		return &OIDCConfig{Scopes: []string{"openid", "profile", "email"}, GroupsClaim: "groups", ButtonLabel: "Sign in with SSO",
			AllowedGroups: []string{}, AdminGroups: []string{}}, err
	}
	if c.AllowedGroups == nil {
		c.AllowedGroups = []string{}
	}
	if c.AdminGroups == nil {
		c.AdminGroups = []string{}
	}
	c.HasClientSecret = s.settings.HasSecret(ctx, oidcSecretKey)
	c.ClientSecret = nil
	c.RedirectURI = ""
	return &c, nil
}

func (s *Service) SetOIDCConfig(ctx context.Context, p *auth.Principal, c OIDCConfig) (*OIDCConfig, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	var v apperr.Validation
	// Keep the issuer exactly as the provider publishes it: go-oidc compares it byte for byte,
	// and many providers (Authentik, Zitadel, Auth0) end theirs with "/".
	c.Issuer = strings.TrimSpace(c.Issuer)
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.RedirectURI = ""
	if c.Enabled {
		if !strings.HasPrefix(c.Issuer, "https://") && !strings.HasPrefix(c.Issuer, "http://") {
			v.Add("issuer", "Issuer must be a URL, e.g. https://auth.example.com/application/o/docveta/")
		}
		if c.ClientID == "" {
			v.Add("client_id", "Client ID is required")
		}
	}
	if !slices.Contains(c.Scopes, "openid") {
		c.Scopes = append([]string{"openid"}, c.Scopes...)
	}
	if c.GroupsClaim == "" {
		c.GroupsClaim = "groups"
	}
	if c.ButtonLabel == "" {
		c.ButtonLabel = "Sign in with SSO"
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if c.Enabled {
		// Validate discovery now so admins get immediate feedback.
		prov, issuer, err := discover(c.Issuer)
		if err != nil {
			return nil, apperr.Invalid("issuer", "Could not load the OpenID configuration from "+
				strings.TrimRight(c.Issuer, "/")+"/.well-known/openid-configuration: "+err.Error())
		}
		c.Issuer = issuer // with or without the trailing "/", as the provider has it
		providerMu.Lock()
		providerCache[issuer] = prov
		providerMu.Unlock()
	}
	if c.ClientSecret != nil {
		if err := s.settings.SetSecret(ctx, oidcSecretKey, *c.ClientSecret); err != nil {
			return nil, err
		}
	}
	c.ClientSecret = nil
	if err := s.settings.Set(ctx, oidcSettingsKey, c, p.UserID); err != nil {
		return nil, err
	}
	s.audit.Record(ctx, nil, "settings.oidc", "settings", oidcSettingsKey, map[string]any{"enabled": c.Enabled, "issuer": c.Issuer})
	return s.OIDCConfig(ctx)
}

var (
	providerMu    sync.Mutex
	providerCache = map[string]*oidc.Provider{}
)

// discover loads the provider's OpenID configuration. Administrators paste the issuer with or
// without a trailing "/" while the provider insists on one form, so both are tried; the
// returned issuer is the one that matched.
func discover(issuer string) (*oidc.Provider, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, issuer)
	if err == nil {
		return p, issuer, nil
	}
	alt := strings.TrimRight(issuer, "/")
	if alt == issuer {
		alt = issuer + "/"
	}
	if p2, err2 := oidc.NewProvider(ctx, alt); err2 == nil {
		return p2, alt, nil
	}
	return nil, issuer, err
}

func provider(issuer string) (*oidc.Provider, error) {
	providerMu.Lock()
	defer providerMu.Unlock()
	if p, ok := providerCache[issuer]; ok {
		return p, nil
	}
	p, _, err := discover(issuer)
	if err != nil {
		return nil, err
	}
	providerCache[issuer] = p
	return p, nil
}

func (s *Service) oauthConfig(ctx context.Context, c *OIDCConfig, redirectURL string) (*oauth2.Config, *oidc.Provider, error) {
	prov, err := provider(c.Issuer)
	if err != nil {
		s.log.Warn("single sign-on provider unreachable", "issuer", c.Issuer, "err", err)
		return nil, nil, apperr.Unavailable("oidc_unavailable", "The single sign-on provider can't be reached right now")
	}
	secret, err := s.settings.GetSecret(ctx, oidcSecretKey)
	if err != nil {
		return nil, nil, err
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: secret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  redirectURL,
		Scopes:       c.Scopes,
	}, prov, nil
}

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	Redirect string `json:"u"`           // redirect URI sent to the provider; the token exchange must repeat it
	LinkUser string `json:"l,omitempty"` // a signed-in person connecting SSO to their account
	App      string `json:"a,omitempty"` // the Android app's PKCE code challenge, when it started the sign-in
	Exp      int64  `json:"e"`
}

// OIDCStartOptions describe a sign-in (or account-linking) attempt.
type OIDCStartOptions struct {
	// BaseURL is the address the person is using, e.g. https://docs.example.com.
	BaseURL  string
	ReturnTo string
	// LinkUser connects the provider account to this signed-in user instead of signing in.
	LinkUser uuid.UUID
	// AppChallenge is set by the Android app (base64url SHA-256 of a secret it keeps): the
	// sign-in then ends with an app link carrying a code only that secret can redeem.
	AppChallenge string
}

// OIDCStart returns the provider URL to redirect to and an opaque, encrypted state
// value to store in a short-lived cookie.
func (s *Service) OIDCStart(ctx context.Context, o OIDCStartOptions) (redirect, cookie string, err error) {
	c, err := s.OIDCConfig(ctx)
	if err != nil {
		return "", "", err
	}
	if !c.Enabled {
		return "", "", apperr.NotFound("Single sign-on")
	}
	st := oidcState{
		State:    randomString(),
		Nonce:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: safeReturnTo(o.ReturnTo),
		Redirect: strings.TrimRight(o.BaseURL, "/") + OIDCCallbackPath,
		App:      o.AppChallenge,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	if o.LinkUser != uuid.Nil {
		st.LinkUser = o.LinkUser.String()
	}
	oc, _, err := s.oauthConfig(ctx, c, st.Redirect)
	if err != nil {
		return "", "", err
	}
	raw, _ := json.Marshal(st)
	cookie = base64.RawURLEncoding.EncodeToString(s.keys.Encrypt(raw))
	redirect = oc.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	return redirect, cookie, nil
}

// OIDCStateFromApp reports whether a sign-in state cookie was made for the Android app, so a
// failed sign-in can be reported back to the app rather than the web login page.
func (s *Service) OIDCStateFromApp(cookie string) bool {
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return false
	}
	raw, err := s.keys.Decrypt(sealed)
	if err != nil {
		return false
	}
	var st oidcState
	return json.Unmarshal(raw, &st) == nil && st.App != ""
}

func randomString() string {
	t, _ := crypto.NewToken("x")
	return t[2:]
}

// safeReturnTo only allows same-origin relative paths to prevent open redirects.
// Sign-in pages are never a place to return to: older web apps sent "/login?redirect=…" here,
// which put people back on the login page after signing in.
func safeReturnTo(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	path, _, _ := strings.Cut(p, "?")
	if path == "/login" || path == "/setup" || strings.HasPrefix(path, "/login/") || strings.HasPrefix(path, "/invite/") {
		return "/"
	}
	return p
}

// OIDCResult is the outcome of a provider callback.
type OIDCResult struct {
	Token    string // the new session; empty when an account was linked
	ReturnTo string
	Linked   bool   // the provider account was connected to the signed-in user
	App      string // the Android app's code challenge when it started the sign-in
}

// OIDCCallback completes the sign-in (or account linking). current is whoever is already
// signed in on this browser, if anyone; linking requires it to be the person who started.
func (s *Service) OIDCCallback(ctx context.Context, cookie, state, code, userAgent string, current *auth.Principal) (*OIDCResult, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return nil, errors.New("missing or invalid sign-in state; please try again")
	}
	raw, err := s.keys.Decrypt(sealed)
	if err != nil {
		return nil, errors.New("invalid sign-in state; please try again")
	}
	var st oidcState
	if err := json.Unmarshal(raw, &st); err != nil || st.State != state || time.Now().Unix() > st.Exp {
		return nil, errors.New("the sign-in took too long or was started in another tab; please try again")
	}
	c, err := s.OIDCConfig(ctx)
	if err != nil || !c.Enabled {
		return nil, errors.New("single sign-on is not enabled")
	}
	oc, prov, err := s.oauthConfig(ctx, c, st.Redirect)
	if err != nil {
		return nil, err
	}
	xctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tok, err := oc.Exchange(xctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "invalid_client" || re.ErrorCode == "unauthorized_client") {
			return nil, errors.New("the identity provider rejected Docveta's client ID or secret; ask an administrator to check them")
		}
		return nil, fmt.Errorf("the identity provider didn't accept the sign-in: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New(`the identity provider didn't return an ID token (is the "openid" scope allowed?)`)
	}
	idt, err := prov.Verifier(&oidc.Config{ClientID: c.ClientID}).Verify(xctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid ID token: %w", err)
	}
	if idt.Nonce != st.Nonce {
		return nil, errors.New("nonce mismatch")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, err
	}
	// Many providers put email or groups only in the userinfo response.
	_, hasEmail := claims["email"]
	_, hasGroups := claims[c.GroupsClaim]
	if !hasEmail || (!hasGroups && (len(c.AllowedGroups) > 0 || len(c.AdminGroups) > 0)) {
		if ui, err := prov.UserInfo(xctx, oauth2.StaticTokenSource(tok)); err == nil {
			var extra map[string]any
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, exists := claims[k]; !exists {
						claims[k] = v
					}
				}
			}
		}
	}
	if st.LinkUser != "" {
		uid, _ := uuid.Parse(st.LinkUser)
		if current == nil || current.Kind != auth.KindSession || current.UserID != uid {
			return nil, errors.New("sign in to Docveta first, then connect single sign-on in Settings → Security")
		}
		if err := s.linkIdentity(ctx, uid, idt.Issuer, idt.Subject, claims); err != nil {
			return nil, err
		}
		return &OIDCResult{ReturnTo: st.ReturnTo, Linked: true}, nil
	}
	userID, err := s.resolveOIDCUser(ctx, c, idt.Issuer, idt.Subject, claims)
	if err != nil {
		return nil, err
	}
	token, err := s.CreateSession(ctx, userID, userAgent, "oidc")
	if err != nil {
		return nil, err
	}
	return &OIDCResult{Token: token, ReturnTo: st.ReturnTo, App: st.App}, nil
}

func claimString(claims map[string]any, k string) string {
	if v, ok := claims[k].(string); ok {
		return v
	}
	return ""
}

func claimVerified(claims map[string]any) bool {
	switch v := claims["email_verified"].(type) {
	case bool:
		return v
	case string: // e.g. AWS Cognito
		return strings.EqualFold(v, "true")
	}
	return false
}

func claimGroups(claims map[string]any, k string) []string {
	switch v := claims[k].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, g := range v {
			if s, ok := g.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Fields(strings.ReplaceAll(v, ",", " "))
	}
	return nil
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func (s *Service) resolveOIDCUser(ctx context.Context, c *OIDCConfig, issuer, subject string, claims map[string]any) (uuid.UUID, error) {
	email := strings.ToLower(claimString(claims, "email"))
	verified := claimVerified(claims)
	name := claimString(claims, "name")
	if name == "" {
		name = claimString(claims, "preferred_username")
	}
	if name == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	groups := claimGroups(claims, c.GroupsClaim)
	if len(c.AllowedGroups) > 0 && !intersects(groups, c.AllowedGroups) {
		return uuid.Nil, errors.New("your account is not in a group that is allowed to use Docveta")
	}
	isAdminGroup := len(c.AdminGroups) > 0 && intersects(groups, c.AdminGroups)

	var userID uuid.UUID
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT u.id, u.status FROM user_identities i JOIN users u ON u.id=i.user_id
			WHERE i.provider=$1 AND i.subject=$2`, issuer, subject).Scan(&userID, &status)
		switch {
		case err == nil:
			if status != "active" {
				return errors.New("this account is disabled; ask an administrator")
			}
		case db.IsNoRows(err):
			// Not linked yet.
			if email != "" && verified && c.LinkByVerifiedEmail {
				err := tx.QueryRow(ctx, `SELECT id, status FROM users WHERE email=$1`, email).Scan(&userID, &status)
				if err == nil && status != "active" {
					return errors.New("this account is disabled; ask an administrator")
				}
				if err != nil && !db.IsNoRows(err) {
					return err
				}
			}
			if userID == uuid.Nil {
				if !c.AutoProvision {
					return errors.New("no Docveta account is connected to this sign-in; ask an administrator to invite you or create your account")
				}
				if email == "" {
					return errors.New(`the identity provider did not share an email address (is the "email" scope allowed?)`)
				}
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists); err != nil {
					return err
				}
				if exists {
					return errors.New("an account with this email already exists; sign in with your password and connect single sign-on in Settings → Security, or ask an administrator")
				}
				id, err := s.createUser(ctx, tx, NewUser{Email: email, DisplayName: name, IsAdmin: isAdminGroup})
				if err != nil {
					return err
				}
				userID = id
			}
			if _, err := tx.Exec(ctx, `INSERT INTO user_identities (id, user_id, provider, subject, email, email_verified)
				VALUES ($1,$2,$3,$4,$5,$6)`, uuid.Must(uuid.NewV7()), userID, issuer, subject, email, verified); err != nil {
				return err
			}
		default:
			return err
		}
		if isAdminGroup {
			// Admin groups only ever grant admin; removing admin is a deliberate manual action.
			_, err := tx.Exec(ctx, `UPDATE users SET is_admin=true WHERE id=$1 AND NOT is_admin`, userID)
			return err
		}
		return nil
	})
	return userID, err
}

// Identity is a single sign-on account connected to a user.
type Identity struct {
	ID        uuid.UUID `json:"id"`
	Provider  string    `json:"provider"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Identities lists the single sign-on accounts connected to the signed-in user.
func (s *Service) Identities(ctx context.Context, p *auth.Principal) ([]Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, provider, coalesce(email,''), created_at FROM user_identities WHERE user_id=$1 ORDER BY created_at`, p.UserID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Identity, error) {
		var i Identity
		return i, r.Scan(&i.ID, &i.Provider, &i.Email, &i.CreatedAt)
	})
}

// Unlink disconnects a single sign-on account, unless it is the only way left to sign in.
func (s *Service) Unlink(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	passwordOK := s.PasswordLoginAllowed(ctx)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var hasPassword bool
		var others int
		if err := tx.QueryRow(ctx, `SELECT u.password_hash IS NOT NULL,
				(SELECT count(*) FROM user_identities WHERE user_id=u.id AND id<>$2)
			FROM users u WHERE u.id=$1`, p.UserID, id).Scan(&hasPassword, &others); err != nil {
			return err
		}
		if (!hasPassword || !passwordOK) && others == 0 {
			return apperr.Conflict("last_login_method", "This is the only way you can sign in. Set a password first.")
		}
		tag, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE id=$1 AND user_id=$2`, id, p.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Connected account")
		}
		s.audit.Record(ctx, tx, "user.identity_unlink", "user", p.UserID.String(), nil)
		return nil
	})
}

func (s *Service) linkIdentity(ctx context.Context, userID uuid.UUID, issuer, subject string, claims map[string]any) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider=$1 AND subject=$2`, issuer, subject).Scan(&owner)
		switch {
		case err == nil && owner == userID:
			return nil // already connected
		case err == nil:
			return errors.New("this single sign-on account is already connected to another Docveta user")
		case !db.IsNoRows(err):
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_identities (id, user_id, provider, subject, email, email_verified) VALUES ($1,$2,$3,$4,$5,$6)`,
			uuid.Must(uuid.NewV7()), userID, issuer, subject, strings.ToLower(claimString(claims, "email")), claimVerified(claims)); err != nil {
			return err
		}
		s.audit.Record(ctx, tx, "user.identity_link", "user", userID.String(), map[string]any{"provider": issuer})
		return nil
	})
}

type appCode struct {
	Token     string `json:"t"`
	Challenge string `json:"c"`
	Exp       int64  `json:"e"`
}

// AppCode seals a new session for the Android app into a short-lived code for its app link.
func (s *Service) AppCode(token, challenge string) string {
	raw, _ := json.Marshal(appCode{Token: token, Challenge: challenge, Exp: time.Now().Add(2 * time.Minute).Unix()})
	return base64.RawURLEncoding.EncodeToString(s.keys.Encrypt(raw))
}

// RedeemAppCode returns the session sealed in code if verifier matches its challenge (PKCE S256).
func (s *Service) RedeemAppCode(code, verifier string) (string, error) {
	bad := apperr.Unauthorized("This sign-in link has expired or isn't valid. Please sign in again.")
	sealed, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return "", bad
	}
	raw, err := s.keys.Decrypt(sealed)
	if err != nil {
		return "", bad
	}
	var c appCode
	if json.Unmarshal(raw, &c) != nil || time.Now().Unix() > c.Exp || c.Challenge == "" || oauth2.S256ChallengeFromVerifier(verifier) != c.Challenge {
		return "", bad
	}
	return c.Token, nil
}
