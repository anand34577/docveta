package api

import (
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/exchange"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/notify"
	"github.com/anand34577/docveta/internal/office"
	"github.com/anand34577/docveta/internal/pipeline"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/platform/settings"
)

func admin(p *auth.Principal) error {
	if !p.Admin() {
		return apperr.Forbidden("Administrator access required")
	}
	return nil
}

type systemInfo struct {
	Version       string               `json:"version"`
	GoVersion     string               `json:"go_version"`
	Platform      string               `json:"platform"`
	DatabaseBytes int64                `json:"database_bytes"`
	BlobFreeBytes int64                `json:"storage_free_bytes"`
	Documents     int64                `json:"documents"`
	Pages         int64                `json:"pages"`
	Users         int64                `json:"users"`
	StorageBytes  int64                `json:"storage_bytes"`
	Queue         *pipeline.QueueStats `json:"queue"`
	WorkersOnline int                  `json:"workers_online"`
	WorkersTotal  int                  `json:"workers_total"`
}

func (a *API) registerAdmin(mux router) {
	// Users
	mux.HandleFunc("GET /api/v1/admin/users", handle(func(r *http.Request, p *auth.Principal) (list[*identity.User], error) {
		u, err := a.Identity.ListUsers(r.Context(), p)
		return items(u), err
	}))
	mux.HandleFunc("POST /api/v1/admin/users", handle(func(r *http.Request, p *auth.Principal) (*identity.User, error) {
		in, err := decode[identity.NewUser](r)
		if err != nil {
			return nil, err
		}
		return a.Identity.CreateUser(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", handle(func(r *http.Request, p *auth.Principal) (*identity.User, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[identity.AdminUserUpdate](r)
		if err != nil {
			return nil, err
		}
		return a.Identity.AdminUpdateUser(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Identity.DeleteUser(r.Context(), p, id)
	}))

	// Settings
	mux.HandleFunc("GET /api/v1/admin/settings/oidc", handle(func(r *http.Request, p *auth.Principal) (*identity.OIDCConfig, error) {
		if err := admin(p); err != nil {
			return nil, err
		}
		return a.withRedirectURI(r)(a.Identity.OIDCConfig(r.Context()))
	}))
	mux.HandleFunc("PUT /api/v1/admin/settings/oidc", handle(func(r *http.Request, p *auth.Principal) (*identity.OIDCConfig, error) {
		in, err := decode[identity.OIDCConfig](r)
		if err != nil {
			return nil, err
		}
		return a.withRedirectURI(r)(a.Identity.SetOIDCConfig(r.Context(), p, in))
	}))
	mux.HandleFunc("GET /api/v1/admin/settings/smtp", handle(func(r *http.Request, p *auth.Principal) (notify.SMTPConfig, error) {
		return a.Notify.SMTPSettings(r.Context(), p)
	}))
	mux.HandleFunc("PUT /api/v1/admin/settings/smtp", handle(func(r *http.Request, p *auth.Principal) (notify.SMTPConfig, error) {
		in, err := decode[notify.SMTPConfig](r)
		if err != nil {
			return notify.SMTPConfig{}, err
		}
		out, err := a.Notify.SetSMTPSettings(r.Context(), p, in)
		if err == nil {
			a.Audit.Record(r.Context(), nil, "settings.smtp", "settings", "smtp", map[string]any{"enabled": in.Enabled, "host": in.Host})
		}
		return out, err
	}))
	mux.HandleFunc("POST /api/v1/admin/settings/smtp/test", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		return a.Notify.SendTestEmail(r.Context(), p)
	}))
	mux.HandleFunc("GET /api/v1/admin/settings/processing", handle(func(r *http.Request, p *auth.Principal) (pipeline.Settings, error) {
		return a.Pipeline.Settings(r.Context(), p)
	}))
	mux.HandleFunc("PUT /api/v1/admin/settings/processing", handle(func(r *http.Request, p *auth.Principal) (pipeline.Settings, error) {
		in, err := decode[pipeline.Settings](r)
		if err != nil {
			return pipeline.Settings{}, err
		}
		return a.Pipeline.SetSettings(r.Context(), p, in)
	}))

	// Export everything as a zip (the same format as `docveta export`).
	mux.HandleFunc("GET /api/v1/admin/export", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		if err := admin(p); err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="docveta-export-`+time.Now().Format("2006-01-02")+`.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		if _, err := exchange.Export(r.Context(), a.Pool, a.Store, exchange.ZipSink(w), exchange.ExportOptions{Version: a.Version}); err != nil {
			httpx.Logger(r.Context()).Error("export failed", "err", err) // the download is already under way; the zip will be incomplete
		}
		a.Audit.Record(r.Context(), nil, "admin.export", "instance", "", nil)
	})

	// Office documents (Gotenberg)
	mux.HandleFunc("GET /api/v1/admin/settings/office", handle(func(r *http.Request, p *auth.Principal) (*office.Info, error) {
		return a.Office.Info(r.Context(), p)
	}))
	mux.HandleFunc("PUT /api/v1/admin/settings/office", handleAction(func(r *http.Request, p *auth.Principal) (*office.Info, error) {
		in, err := decode[office.Settings](r)
		if err != nil {
			return nil, err
		}
		return a.Office.Set(r.Context(), p, in.URL)
	}))
	mux.HandleFunc("POST /api/v1/admin/settings/office/test", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		in, err := decode[office.Settings](r)
		if err != nil {
			return err
		}
		return a.Office.Test(r.Context(), p, in.URL)
	}))

	// Workers
	mux.HandleFunc("GET /api/v1/admin/workers", handle(func(r *http.Request, p *auth.Principal) (list[pipeline.Worker], error) {
		w, err := a.Pipeline.ListWorkers(r.Context(), p)
		return items(w), err
	}))
	mux.HandleFunc("POST /api/v1/admin/workers", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		in, err := decode[struct {
			Name string `json:"name"`
		}](r)
		if err != nil {
			return nil, err
		}
		w, token, err := a.Pipeline.CreateWorker(r.Context(), p, in.Name)
		if err != nil {
			return nil, err
		}
		a.Audit.Record(r.Context(), nil, "worker.create", "worker", w.ID.String(), map[string]any{"name": w.Name})
		return map[string]any{"worker": w, "token": token}, nil
	}))
	mux.HandleFunc("PATCH /api/v1/admin/workers/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		in, err := decode[struct {
			Enabled bool `json:"enabled"`
		}](r)
		if err != nil {
			return err
		}
		return a.Pipeline.SetWorkerEnabled(r.Context(), p, id, in.Enabled)
	}))
	mux.HandleFunc("POST /api/v1/admin/workers/{id}/rotate-token", handle(func(r *http.Request, p *auth.Principal) (map[string]string, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		token, err := a.Pipeline.RotateWorkerToken(r.Context(), p, id)
		if err == nil {
			a.Audit.Record(r.Context(), nil, "worker.rotate_token", "worker", id.String(), nil)
		}
		return map[string]string{"token": token}, err
	}))
	mux.HandleFunc("DELETE /api/v1/admin/workers/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		if err := a.Pipeline.DeleteWorker(r.Context(), p, id); err != nil {
			return err
		}
		a.Audit.Record(r.Context(), nil, "worker.delete", "worker", id.String(), nil)
		return nil
	}))

	// Processing tasks
	mux.HandleFunc("GET /api/v1/admin/tasks", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		t, st, err := a.Pipeline.Tasks(r.Context(), p, r.URL.Query().Get("status"), httpx.QueryInt(r, "limit", 100, 1, 500))
		if t == nil {
			t = []pipeline.TaskView{}
		}
		return map[string]any{"items": t, "stats": st}, err
	}))
	mux.HandleFunc("POST /api/v1/admin/tasks/{id}/retry", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Pipeline.RetryTask(r.Context(), p, id)
	}))

	// System & audit
	mux.HandleFunc("GET /api/v1/admin/settings/server", handle(func(r *http.Request, p *auth.Principal) (*serverSettings, error) {
		if err := admin(p); err != nil {
			return nil, err
		}
		return a.serverSettings(r), nil
	}))
	mux.HandleFunc("PUT /api/v1/admin/settings/server", handle(func(r *http.Request, p *auth.Principal) (*serverSettings, error) {
		if err := admin(p); err != nil {
			return nil, err
		}
		in, err := decode[settings.Server](r)
		if err != nil {
			return nil, err
		}
		if u := strings.TrimSpace(in.PublicURL); u != "" {
			pu, err := url.Parse(u)
			if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
				return nil, apperr.Invalid("public_url", "Enter the full address, e.g. https://docs.example.com or http://192.168.1.20:8080")
			}
		}
		if err := a.Settings.SetServerSettings(r.Context(), in, p.UserID); err != nil {
			return nil, err
		}
		a.Audit.Record(r.Context(), nil, "settings.server", "settings", "server", map[string]any{"public_url": in.PublicURL, "allow_local_targets": in.AllowLocalTargets})
		return a.serverSettings(r), nil
	}))
	mux.HandleFunc("GET /api/v1/admin/system", handle(func(r *http.Request, p *auth.Principal) (*systemInfo, error) {
		if err := admin(p); err != nil {
			return nil, err
		}
		s := &systemInfo{Version: a.Version, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
			BlobFreeBytes: a.Store.FreeBytes()}
		if err := a.Pool.QueryRow(r.Context(), `SELECT pg_database_size(current_database()),
			(SELECT count(*) FROM documents WHERE deleted_at IS NULL), (SELECT count(*) FROM pages), (SELECT count(*) FROM users),
			(SELECT coalesce(sum(size_bytes),0) FROM document_files)`).Scan(&s.DatabaseBytes, &s.Documents, &s.Pages, &s.Users, &s.StorageBytes); err != nil {
			return nil, err
		}
		_, st, err := a.Pipeline.Tasks(r.Context(), p, "none", 1)
		if err != nil {
			return nil, err
		}
		s.Queue = st
		ws, err := a.Pipeline.ListWorkers(r.Context(), p)
		if err != nil {
			return nil, err
		}
		s.WorkersTotal = len(ws)
		for _, w := range ws {
			if w.Online {
				s.WorkersOnline++
			}
		}
		return s, nil
	}))
	mux.HandleFunc("GET /api/v1/admin/audit", handle(func(r *http.Request, p *auth.Principal) (list[audit.Entry], error) {
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		e, err := a.Audit.List(r.Context(), p, r.URL.Query().Get("action"), before, httpx.QueryInt(r, "limit", 100, 1, 500))
		return items(e), err
	}))
}

// withRedirectURI fills in the callback address to register with the identity provider, as the
// administrator's browser reaches Docveta (exactly what sign-ins from there will send).
func (a *API) withRedirectURI(r *http.Request) func(*identity.OIDCConfig, error) (*identity.OIDCConfig, error) {
	return func(c *identity.OIDCConfig, err error) (*identity.OIDCConfig, error) {
		if c != nil {
			c.RedirectURI = a.publicBase(r) + identity.OIDCCallbackPath
		}
		return c, err
	}
}

type serverSettings struct {
	settings.Server
	// BaseURL is DOCVETA_BASE_URL; when it names a real host it is used and PublicURL is ignored.
	BaseURL      string `json:"base_url"`
	BaseURLFixed bool   `json:"base_url_fixed"`
	// Detected is the address this request used.
	Detected string `json:"detected"`
	// AllowLocalEnv is DOCVETA_ALLOW_LOCAL_TARGETS (allows local targets whatever the setting says).
	AllowLocalEnv bool `json:"allow_local_env"`
}

func (a *API) serverSettings(r *http.Request) *serverSettings {
	return &serverSettings{Server: a.Settings.ServerSettings(r.Context()), BaseURL: a.Cfg.BaseURL.String(),
		BaseURLFixed: !isLoopbackHost(a.Cfg.BaseURL.Hostname()), Detected: a.publicBase(r), AllowLocalEnv: a.Cfg.AllowLocalTargets}
}
