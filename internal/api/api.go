// Package api wires HTTP routes to services. Handlers are thin: decode, call a
// service, encode. All authorization happens in the services.
package api

import (
	_ "embed"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/ai"
	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/docedit"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/folders"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/notify"
	"github.com/anand34577/docveta/internal/office"
	"github.com/anand34577/docveta/internal/pipeline"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/shares"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/storage"
	"github.com/anand34577/docveta/internal/taxonomy"
	"github.com/anand34577/docveta/internal/tus"
	"github.com/anand34577/docveta/internal/views"
	"github.com/anand34577/docveta/internal/workflows"
)

type Deps struct {
	Cfg          *config.Config
	Log          *slog.Logger
	Pool         *pgxpool.Pool
	Store        storage.Store
	Identity     *identity.Service
	Spaces       *spaces.Service
	Taxonomy     *taxonomy.Service
	CustomFields *customfields.Service
	Documents    *documents.Service
	Docedit      *docedit.Service
	Search       *search.Service
	Pipeline     *pipeline.Service
	Notify       *notify.Service
	Views        *views.Service
	Shares       *shares.Service
	AI           *ai.Service
	Office       *office.Converter
	Folders      *folders.Service
	Workflows    *workflows.Service
	Uploads      *tus.Service
	Queue        *jobs.Queue
	Audit        *audit.Log
	Version      string
}

type API struct {
	Deps
	cookieName string
	origin     string
}

func New(d Deps) *API {
	name := "docveta_session"
	if d.Cfg.SecureCookies() {
		name = "__Host-docveta_session" // __Host- requires Secure, Path=/ and no Domain
	}
	u := *d.Cfg.BaseURL
	return &API{Deps: d, cookieName: name, origin: u.Scheme + "://" + u.Host}
}

// router is satisfied by *http.ServeMux; tests record the registered patterns.
type router interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// OpenAPISpec is the API contract (OpenAPI 3.0.3), served at /api/v1/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte

// Register adds all API routes to mux.
func (a *API) Register(mux router) {
	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(OpenAPISpec)
	})
	a.registerAuth(mux)
	a.registerAuth2(mux)
	a.registerSpaces(mux)
	a.registerTaxonomy(mux)
	a.registerCustomFields(mux)
	a.registerDocuments(mux)
	a.registerDocEdit(mux)
	a.registerNotify(mux)
	a.registerShares(mux)
	a.registerAI(mux)
	a.registerMCP(mux)
	a.registerFolders(mux)
	a.registerWorkflows(mux)
	a.registerUploads(mux)
	a.registerBarcodes(mux)
	a.registerAdmin(mux)
	a.registerWorker(mux)
	notFound := func(w http.ResponseWriter, r *http.Request) { httpx.Error(w, r, apperr.NotFound("Endpoint")) }
	mux.HandleFunc("/api/", notFound)
	mux.HandleFunc("/worker/", notFound)
}

// Authenticate resolves the principal from a bearer token or session cookie, and
// enforces CSRF protection for cookie-authenticated unsafe requests.
func (a *API) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := audit.WithUserAgent(r.Context(), r.UserAgent())
		var p *auth.Principal
		var err error
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			switch {
			case strings.HasPrefix(tok, "dvt_wrk_"):
				p, err = a.Pipeline.AuthenticateWorker(ctx, tok)
			default:
				p, err = a.Identity.AuthenticateToken(ctx, tok)
			}
			if err == nil && p == nil {
				err = apperr.Unauthorized("Invalid or expired token")
			}
		} else if c, cerr := r.Cookie(a.cookieName); cerr == nil && c.Value != "" {
			p, err = a.Identity.AuthenticateSession(ctx, c.Value)
			if err == nil && p != nil && !safeMethod(r.Method) && !a.sameOrigin(r) {
				err = apperr.Forbidden("Cross-site request blocked")
			}
			if err == nil && p == nil {
				a.clearSessionCookie(w) // expired/revoked: let the UI show the login page cleanly
			}
		}
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if p != nil {
			ctx = auth.With(ctx, p)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin implements CSRF protection using Fetch Metadata with an Origin fallback.
func (a *API) sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "cross-site", "same-site":
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		if a.Cfg.DevMode {
			if u, err := url.Parse(o); err == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {
				return true
			}
		}
		return o == a.origin
	}
	return true // non-browser client
}

func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: a.cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.Cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode, MaxAge: int(a.Cfg.SessionMax.Seconds()),
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.Cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// user returns the authenticated user principal or writes 401.
func user(w http.ResponseWriter, r *http.Request) *auth.Principal {
	p := auth.From(r.Context())
	if p == nil || (p.Kind != auth.KindSession && p.Kind != auth.KindToken) {
		httpx.Error(w, r, apperr.Unauthorized(""))
		return nil
	}
	return p
}

// handler adapts a function returning (value, error) into an http.HandlerFunc that
// requires a signed-in user.
func handle[T any](fn func(r *http.Request, p *auth.Principal) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		v, err := fn(r, p)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		httpx.JSON(w, status, v)
	}
}

// handleAction is like handle for POST actions on existing things (200, not 201 Created).
func handleAction[T any](fn func(r *http.Request, p *auth.Principal) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		v, err := fn(r, p)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, v)
	}
}

// handleNoContent is like handle for operations without a response body.
func handleNoContent(fn func(r *http.Request, p *auth.Principal) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		if err := fn(r, p); err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.NoContent(w)
	}
}

// decode decodes a JSON body into a new T.
func decode[T any](r *http.Request) (T, error) {
	var v T
	err := httpx.Decode(r, &v)
	return v, err
}

type list[T any] struct {
	Items []T `json:"items"`
}

func items[T any](v []T) list[T] {
	if v == nil {
		v = []T{}
	}
	return list[T]{Items: v}
}
