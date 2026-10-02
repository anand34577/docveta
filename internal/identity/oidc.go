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
	return &c, nil
}

func (s *Service) SetOIDCConfig(ctx context.Context, p *auth.Principal, c OIDCConfig) (*OIDCConfig, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	var v apperr.Validation
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
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
	if c.ClientSecret != nil {
		if err := s.settings.SetSecret(ctx, oidcSecretKey, *c.ClientSecret); err != nil {
			return nil, err
		}
	}
	if c.Enabled {
		// Validate discovery now so admins get immediate feedback.
		if _, err := s.provider(ctx, c.Issuer, true); err != nil {
			return nil, apperr.Invalid("issuer", "Could not load OpenID configuration from the issuer: "+err.Error())
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

func (s *Service) provider(ctx context.Context, issuer string, refresh bool) (*oidc.Provider, error) {
	providerMu.Lock()
	defer providerMu.Unlock()
	if p, ok := providerCache[issuer]; ok && !refresh {
		return p, nil
	}
	dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(dctx, issuer)
	if err != nil {
		return nil, err
	}
	providerCache[issuer] = p
	return p, nil
}

func (s *Service) oauthConfig(ctx context.Context, c *OIDCConfig) (*oauth2.Config, *oidc.Provider, error) {
	prov, err := s.provider(ctx, c.Issuer, false)
	if err != nil {
		return nil, nil, apperr.Unavailable("oidc_unavailable", "Single sign-on provider is unreachable")
	}
	secret, err := s.settings.GetSecret(ctx, oidcSecretKey)
	if err != nil {
		return nil, nil, err
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: secret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  s.cfg.BaseURL.String() + "/api/v1/auth/oidc/callback",
		Scopes:       c.Scopes,
	}, prov, nil
}

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
	Exp      int64  `json:"e"`
}

// OIDCStart returns the provider URL to redirect to and an opaque, encrypted state
// value to store in a short-lived cookie.
func (s *Service) OIDCStart(ctx context.Context, returnTo string) (redirect, cookie string, err error) {
	c, err := s.OIDCConfig(ctx)
	if err != nil {
		return "", "", err
	}
	if !c.Enabled {
		return "", "", apperr.NotFound("Single sign-on")
	}
	oc, _, err := s.oauthConfig(ctx, c)
	if err != nil {
		return "", "", err
	}
	st := oidcState{
		State:    randomString(),
		Nonce:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: safeReturnTo(returnTo),
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	raw, _ := json.Marshal(st)
	cookie = base64.RawURLEncoding.EncodeToString(s.keys.Encrypt(raw))
	redirect = oc.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	return redirect, cookie, nil
}

func randomString() string {
	t, _ := crypto.NewToken("x")
	return t[2:]
}

// safeReturnTo only allows same-origin relative paths to prevent open redirects.
func safeReturnTo(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	return p
}

// OIDCCallback completes the login and returns a session token and where to go next.
func (s *Service) OIDCCallback(ctx context.Context, cookie, state, code, userAgent string) (token, returnTo string, err error) {
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return "", "", errors.New("missing or invalid login state; please try again")
	}
	raw, err := s.keys.Decrypt(sealed)
	if err != nil {
		return "", "", errors.New("invalid login state; please try again")
	}
	var st oidcState
	if err := json.Unmarshal(raw, &st); err != nil || st.State != state || time.Now().Unix() > st.Exp {
		return "", "", errors.New("login expired or state mismatch; please try again")
	}
	c, err := s.OIDCConfig(ctx)
	if err != nil || !c.Enabled {
		return "", "", errors.New("single sign-on is not enabled")
	}
	oc, prov, err := s.oauthConfig(ctx, c)
	if err != nil {
		return "", "", err
	}
	xctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tok, err := oc.Exchange(xctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return "", "", fmt.Errorf("token exchange failed: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return "", "", errors.New("provider did not return an ID token")
	}
	idt, err := prov.Verifier(&oidc.Config{ClientID: c.ClientID}).Verify(xctx, rawID)
	if err != nil {
		return "", "", fmt.Errorf("invalid ID token: %w", err)
	}
	if idt.Nonce != st.Nonce {
		return "", "", errors.New("nonce mismatch")
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return "", "", err
	}
	// Some providers only put email/groups in userinfo.
	if _, has := claims["email"]; !has {
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
	userID, err := s.resolveOIDCUser(ctx, c, idt.Issuer, idt.Subject, claims)
	if err != nil {
		return "", "", err
	}
	token, err = s.CreateSession(ctx, userID, userAgent, "oidc")
	return token, st.ReturnTo, err
}

func claimString(claims map[string]any, k string) string {
	if v, ok := claims[k].(string); ok {
		return v
	}
	return ""
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
	verified, _ := claims["email_verified"].(bool)
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
					return errors.New("no Docveta account is linked to this sign-in; ask an administrator to create one")
				}
				if email == "" {
					return errors.New("the identity provider did not share an email address")
				}
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists); err != nil {
					return err
				}
				if exists {
					return errors.New("an account with this email already exists; sign in with your password and link single sign-on from Settings, or ask an administrator")
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
