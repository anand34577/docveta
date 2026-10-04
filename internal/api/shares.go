package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/shares"
)

type shareCreated struct {
	Share *shares.Share `json:"share"`
	Link  string        `json:"link"` // shown once: only a hash is stored
}

func (a *API) registerShares(mux router) {
	mux.HandleFunc("POST /api/v1/shares", handle(func(r *http.Request, p *auth.Principal) (*shareCreated, error) {
		in, err := decode[shares.NewShare](r)
		if err != nil {
			return nil, err
		}
		sh, token, err := a.Shares.Create(r.Context(), p, in)
		if err != nil {
			return nil, err
		}
		return &shareCreated{Share: sh, Link: a.publicBase(r) + "/s/" + token}, nil
	}))
	mux.HandleFunc("GET /api/v1/shares", handle(func(r *http.Request, p *auth.Principal) (list[*shares.Share], error) {
		var doc, view *uuid.UUID
		for name, dst := range map[string]**uuid.UUID{"document_id": &doc, "view_id": &view} {
			if s := r.URL.Query().Get(name); s != "" {
				id, err := uuid.Parse(s)
				if err != nil {
					return list[*shares.Share]{}, apperr.Invalid(name, "Invalid id")
				}
				*dst = &id
			}
		}
		l, err := a.Shares.List(r.Context(), p, doc, view)
		return items(l), err
	}))
	mux.HandleFunc("DELETE /api/v1/shares/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Shares.Revoke(r.Context(), p, id)
	}))

	// Public: no account needed. Never cached or indexed.
	public := func(w http.ResponseWriter) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
	}
	cookieFor := func(r *http.Request, token string) (name, val string, err error) {
		name, err = a.Shares.CookieName(r.Context(), token)
		if err != nil {
			return "", "", err
		}
		if c, cerr := r.Cookie(name); cerr == nil {
			val = c.Value
		}
		return name, val, nil
	}
	mux.HandleFunc("GET /api/v1/public/shares/{token}", func(w http.ResponseWriter, r *http.Request) {
		public(w)
		token := r.PathValue("token")
		_, val, err := cookieFor(r, token)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		out, err := a.Shares.Meta(r.Context(), token, val)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /api/v1/public/shares/{token}/unlock", func(w http.ResponseWriter, r *http.Request) {
		public(w)
		in, err := decode[struct {
			Password string `json:"password"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		token := r.PathValue("token")
		name, val, ttl, err := a.Shares.Unlock(r.Context(), token, in.Password)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: name, Value: val, Path: "/api/v1/public/shares/" + token, HttpOnly: true,
			Secure: a.Cfg.SecureCookies(), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())})
		httpx.NoContent(w)
	})
	mux.HandleFunc("GET /api/v1/public/shares/{token}/documents/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		public(w)
		token := r.PathValue("token")
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		_, val, err := cookieFor(r, token)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			kind = "best"
		}
		f, err := a.Shares.File(r.Context(), token, val, id, kind, r.URL.Query().Get("download") == "1")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		defer f.Reader.Close()
		writeFile(w, r, f)
	})
}
