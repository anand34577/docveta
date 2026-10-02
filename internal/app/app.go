// Package app assembles Docveta: database, job queue, services, HTTP server.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/anand34577/docveta/internal/api"
	"github.com/anand34577/docveta/internal/audit"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/notify"
	"github.com/anand34577/docveta/internal/pipeline"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/platform/settings"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/storage"
	"github.com/anand34577/docveta/internal/taxonomy"
	"github.com/anand34577/docveta/internal/views"
	"github.com/anand34577/docveta/internal/webui"
)

type App struct {
	Cfg      *config.Config
	Log      *slog.Logger
	Pool     *pgxpool.Pool
	Keys     *crypto.Keys
	Settings *settings.Store
	FS       *storage.FS
	Store    storage.Store
	PDF      *pipeline.PDF
	Queue    *jobs.Queue
	River    *river.Client[pgx.Tx]

	Audit     *audit.Log
	Identity  *identity.Service
	Spaces    *spaces.Service
	Taxonomy  *taxonomy.Service
	Search    *search.Service
	Documents *documents.Service
	Pipeline  *pipeline.Service
	Notify    *notify.Service
	Views     *views.Service

	Version string
}

func systemPrincipal() *auth.Principal { return auth.System() }

// New connects to the database, runs migrations (unless skipMigrate) and builds all services.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, version string, skipMigrate bool) (*App, error) {
	a := &App{Cfg: cfg, Log: log, Version: version}
	var err error
	if a.Pool, err = db.Open(ctx, cfg.DatabaseURL); err != nil {
		return nil, err
	}
	if !skipMigrate {
		if err := db.Migrate(ctx, a.Pool, log); err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}
		migrator, err := rivermigrate.New(riverpgxv5.New(a.Pool), &rivermigrate.Config{Logger: quietLogger(log)})
		if err != nil {
			return nil, err
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return nil, fmt.Errorf("migrate job queue: %w", err)
		}
	}
	if a.Keys, err = crypto.NewKeys(cfg.SecretKey); err != nil {
		return nil, err
	}
	if a.FS, err = storage.NewFS(cfg.DataDir); err != nil {
		return nil, err
	}
	a.Store = a.FS
	a.Settings = settings.New(a.Pool, a.Keys)
	a.Queue = &jobs.Queue{}

	a.Audit = audit.New(a.Pool, log)
	a.Spaces = spaces.NewService(a.Pool)
	a.Identity = identity.NewService(a.Pool, cfg, a.Keys, a.Settings, a.Audit, log)
	a.Taxonomy = taxonomy.NewService(a.Pool, a.Spaces)
	a.Search = search.NewService(a.Pool, a.Spaces)
	a.Documents = documents.NewService(a.Pool, a.Store, a.Spaces, a.Search, a.Queue, log, cfg.MaxUploadBytes)
	a.Notify = notify.NewService(a.Pool, a.Keys, a.Settings, cfg, log)
	a.Views = views.NewService(a.Pool, a.Spaces)
	return a, nil
}

// quietLogger lowers River's chatty info logs to debug.
func quietLogger(log *slog.Logger) *slog.Logger {
	return slog.New(levelShift{log.Handler()})
}

type levelShift struct{ slog.Handler }

func (l levelShift) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelInfo {
		r.Level = slog.LevelDebug
	}
	if !l.Handler.Enabled(ctx, r.Level) {
		return nil
	}
	return l.Handler.Handle(ctx, r)
}

func (l levelShift) Enabled(context.Context, slog.Level) bool { return true }

func (l levelShift) WithAttrs(as []slog.Attr) slog.Handler {
	return levelShift{l.Handler.WithAttrs(as)}
}
func (l levelShift) WithGroup(g string) slog.Handler { return levelShift{l.Handler.WithGroup(g)} }

// startJobs creates the PDF engine, pipeline and River client and starts processing.
func (a *App) startJobs(ctx context.Context) error {
	var err error
	if a.PDF, err = pipeline.NewPDF(a.Cfg.PDFWorkers); err != nil {
		return err
	}
	a.Pipeline = pipeline.NewService(a.Pool, a.Store, a.PDF, a.Queue, a.Settings, a.Log)
	pipeline.ServerVersion = a.Version

	taxonomy.ReindexHook = a.Pipeline.EnqueueReindex
	a.Identity.OnEvent = func(ctx context.Context, e identity.Event) {
		_ = a.Queue.Emit(ctx, jobs.Event{Type: e.Type, Title: e.Title, Body: e.Body, Severity: "warning",
			Recipients: []uuid.UUID{e.UserID}, Link: "/settings/security"})
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &pipeline.PreprocessWorker{S: a.Pipeline})
	river.AddWorker(workers, &pipeline.FinalizeWorker{S: a.Pipeline})
	river.AddWorker(workers, &pipeline.ClassifyWorker{S: a.Pipeline})
	river.AddWorker(workers, &pipeline.ReindexWorker{S: a.Pipeline})
	river.AddWorker(workers, &pipeline.LeaseReaperWorker{S: a.Pipeline})
	river.AddWorker(workers, &notify.Worker{S: a.Notify})
	river.AddWorker(workers, &MaintenanceWorker{A: a})

	a.River, err = river.NewClient(riverpgxv5.New(a.Pool), &river.Config{
		Logger:  quietLogger(a.Log),
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: max(a.Cfg.JobWorkers, 2)}},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(30*time.Second),
				func() (river.JobArgs, *river.InsertOpts) {
					return jobs.LeaseReaperArgs{}, &river.InsertOpts{MaxAttempts: 1}
				},
				&river.PeriodicJobOpts{RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(10*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) {
					return jobs.MaintenanceArgs{}, &river.InsertOpts{MaxAttempts: 1}
				},
				&river.PeriodicJobOpts{RunOnStart: true}),
		},
		JobTimeout: 10 * time.Minute,
	})
	if err != nil {
		return err
	}
	a.Queue.Client = a.River
	return a.River.Start(ctx)
}

// Handler builds the full HTTP handler.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	m := newMetrics(a)
	ap := api.New(api.Deps{
		Cfg: a.Cfg, Log: a.Log, Pool: a.Pool, Store: a.Store, Identity: a.Identity, Spaces: a.Spaces, Taxonomy: a.Taxonomy,
		Documents: a.Documents, Search: a.Search, Pipeline: a.Pipeline, Notify: a.Notify, Views: a.Views, Audit: a.Audit, Version: a.Version,
	})
	ap.Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := a.Pool.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", m.handler())
	mux.Handle("/", webui.Handler())

	return httpx.Chain(mux,
		httpx.Base(a.Log, a.Cfg.TrustedProxies, m.observe),
		httpx.SecurityHeaders(a.Cfg.BaseURL.Scheme == "https"),
		ap.Authenticate,
	)
}

// Serve starts background jobs and the HTTP server, and shuts down gracefully when ctx ends.
func (a *App) Serve(ctx context.Context) error {
	if err := a.startJobs(ctx); err != nil {
		return fmt.Errorf("start jobs: %w", err)
	}
	go a.runLocalOCR(ctx)
	srv := &http.Server{
		Addr:              a.Cfg.ListenAddr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(a.Log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() {
		a.Log.Info("docveta listening", "addr", a.Cfg.ListenAddr, "base_url", a.Cfg.BaseURL.String(), "version", a.Version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	a.Log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	if a.River != nil {
		_ = a.River.Stop(sctx)
	}
	return nil
}

func (a *App) Close() {
	if a.PDF != nil {
		_ = a.PDF.Close()
	}
	if a.Pool != nil {
		a.Pool.Close()
	}
}
