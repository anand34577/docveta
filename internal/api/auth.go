package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/spaces"
)

type statusResponse struct {
	SetupNeeded   bool   `json:"setup_needed"`
	Version       string `json:"version"`
	PasswordLogin bool   `json:"password_login"`
	OIDC          struct {
		Enabled     bool   `json:"enabled"`
		ButtonLabel string `json:"button_label"`
	} `json:"oidc"`
}

type meResponse struct {
	*identity.User
	Spaces []*spaces.Space `json:"spaces"`
}

func (a *API) registerAuth(mux router) {
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		var s statusResponse
		var err error
		if s.SetupNeeded, err = a.Identity.SetupNeeded(r.Context()); err != nil {
			httpx.Error(w, r, err)
			return
		}
		s.Version = a.Version
		s.PasswordLogin = a.Identity.PasswordLoginAllowed(r.Context())
		if c, err := a.Identity.OIDCConfig(r.Context()); err == nil && c.Enabled {
			s.OIDC.Enabled, s.OIDC.ButtonLabel = true, c.ButtonLabel
		}
		httpx.JSON(w, http.StatusOK, s)
	})

	mux.HandleFunc("POST /api/v1/setup", func(w http.ResponseWriter, r *http.Request) {
		in, err := decode[identity.SetupInput](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		u, err := a.Identity.Setup(r.Context(), in)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		token, err := a.Identity.CreateSession(r.Context(), u.ID, r.UserAgent(), "setup")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		a.setSessionCookie(w, token)
		httpx.JSON(w, http.StatusCreated, u)
	})

	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		in, err := decode[struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		res, err := a.Identity.Login(r.Context(), in.Email, in.Password, r.UserAgent())
		if err != nil {
			if ae, ok := apperr.As(err); ok && ae.Kind == apperr.KindRateLimited {
				if s, ok := ae.Extra["retry_after_seconds"].(int); ok {
					w.Header().Set("Retry-After", strconv.Itoa(s))
				}
			}
			httpx.Error(w, r, err)
			return
		}
		if res.Challenge != "" {
			// The password was right but the account needs a second factor: no session yet.
			httpx.JSON(w, http.StatusOK, map[string]any{"two_factor_required": true, "challenge": res.Challenge})
			return
		}
		a.setSessionCookie(w, res.Token)
		httpx.JSON(w, http.StatusOK, res.User)
	})

	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		_ = a.Identity.Logout(r.Context(), auth.From(r.Context()))
		a.clearSessionCookie(w)
		httpx.NoContent(w)
	})

	mux.HandleFunc("GET /api/v1/auth/oidc/start", func(w http.ResponseWriter, r *http.Request) {
		redirect, cookie, err := a.Identity.OIDCStart(r.Context(), r.URL.Query().Get("return_to"))
		if err != nil {
			http.Redirect(w, r, "/login?error="+url.QueryEscape(errMsg(err)), http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "docveta_oidc", Value: cookie, Path: "/api/v1/auth/oidc", HttpOnly: true,
			Secure: a.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: 600})
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	})

	mux.HandleFunc("GET /api/v1/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.SetCookie(w, &http.Cookie{Name: "docveta_oidc", Value: "", Path: "/api/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: a.Cfg.SecureCookies()})
		if e := q.Get("error"); e != "" {
			msg := q.Get("error_description")
			if msg == "" {
				msg = e
			}
			http.Redirect(w, r, "/login?error="+url.QueryEscape("Sign-in was cancelled or denied: "+msg), http.StatusSeeOther)
			return
		}
		c, err := r.Cookie("docveta_oidc")
		if err != nil {
			http.Redirect(w, r, "/login?error="+url.QueryEscape("Your sign-in took too long. Please try again."), http.StatusSeeOther)
			return
		}
		token, returnTo, err := a.Identity.OIDCCallback(r.Context(), c.Value, q.Get("state"), q.Get("code"), r.UserAgent())
		if err != nil {
			httpx.Logger(r.Context()).Warn("oidc callback failed", "err", err)
			http.Redirect(w, r, "/login?error="+url.QueryEscape(errMsg(err)), http.StatusSeeOther)
			return
		}
		a.setSessionCookie(w, token)
		http.Redirect(w, r, returnTo, http.StatusSeeOther)
	})

	// --- Me ---
	mux.HandleFunc("GET /api/v1/me", handle(func(r *http.Request, p *auth.Principal) (*meResponse, error) {
		u, err := a.Identity.GetUser(r.Context(), p.UserID)
		if err != nil {
			return nil, err
		}
		sp, err := a.Spaces.List(r.Context(), p)
		if err != nil {
			return nil, err
		}
		if sp == nil {
			sp = []*spaces.Space{}
		}
		return &meResponse{User: u, Spaces: sp}, nil
	}))
	mux.HandleFunc("PATCH /api/v1/me", handle(func(r *http.Request, p *auth.Principal) (*identity.User, error) {
		in, err := decode[identity.ProfileUpdate](r)
		if err != nil {
			return nil, err
		}
		return a.Identity.UpdateProfile(r.Context(), p, in)
	}))
	mux.HandleFunc("POST /api/v1/me/password", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		in, err := decode[struct {
			Current string `json:"current_password"`
			New     string `json:"new_password"`
		}](r)
		if err != nil {
			return err
		}
		if p.Kind != auth.KindSession {
			return apperr.Forbidden("Passwords can only be changed from a browser session")
		}
		return a.Identity.ChangePassword(r.Context(), p, in.Current, in.New)
	}))
	mux.HandleFunc("GET /api/v1/me/sessions", handle(func(r *http.Request, p *auth.Principal) (list[identity.Session], error) {
		s, err := a.Identity.ListSessions(r.Context(), p)
		return items(s), err
	}))
	mux.HandleFunc("DELETE /api/v1/me/sessions/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		if r.PathValue("id") == "others" {
			return a.Identity.RevokeSession(r.Context(), p, uuid.Nil)
		}
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Identity.RevokeSession(r.Context(), p, id)
	}))
	mux.HandleFunc("GET /api/v1/me/tokens", handle(func(r *http.Request, p *auth.Principal) (list[identity.APIToken], error) {
		t, err := a.Identity.ListTokens(r.Context(), p)
		return items(t), err
	}))
	mux.HandleFunc("POST /api/v1/me/tokens", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		in, err := decode[identity.NewToken](r)
		if err != nil {
			return nil, err
		}
		t, token, err := a.Identity.CreateToken(r.Context(), p, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"token": t, "secret": token}, nil
	}))
	mux.HandleFunc("DELETE /api/v1/me/tokens/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Identity.RevokeToken(r.Context(), p, id)
	}))
	mux.HandleFunc("GET /api/v1/users/directory", handle(func(r *http.Request, p *auth.Principal) (list[identity.DirectoryEntry], error) {
		d, err := a.Identity.Directory(r.Context(), p)
		return items(d), err
	}))
}

func errMsg(err error) string {
	if ae, ok := apperr.As(err); ok {
		return ae.Msg
	}
	return err.Error()
}
