package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/jobs"
)

// MaintenanceWorker runs periodic housekeeping: trash purge, session cleanup, temp
// cleanup, worker offline alerts, low-storage alerts and blob garbage collection.
type MaintenanceWorker struct {
	river.WorkerDefaults[jobs.MaintenanceArgs]
	A *App
}

func (w *MaintenanceWorker) Timeout(*river.Job[jobs.MaintenanceArgs]) time.Duration {
	return 30 * time.Minute
}

func (w *MaintenanceWorker) Work(ctx context.Context, _ *river.Job[jobs.MaintenanceArgs]) error {
	a := w.A
	log := a.Log.With("job", "maintenance")
	if n, err := a.Documents.PurgeExpired(ctx, time.Now().Add(-a.Cfg.TrashRetention)); err != nil {
		log.Error("purge trash", "err", err)
	} else if n > 0 {
		log.Info("purged expired trash", "documents", n)
	}
	if err := a.Identity.CleanupSessions(ctx); err != nil {
		log.Error("cleanup sessions", "err", err)
	}
	a.FS.CleanTemp(24 * time.Hour)
	if err := w.workerAlerts(ctx); err != nil {
		log.Error("worker alerts", "err", err)
	}
	w.storageAlert(ctx)
	if err := w.gcBlobs(ctx); err != nil {
		log.Error("blob gc", "err", err)
	}
	return nil
}

func (w *MaintenanceWorker) workerAlerts(ctx context.Context) error {
	a := w.A
	cfg, _ := a.Pipeline.Settings(ctx, systemPrincipal())
	rows, err := a.Pool.Query(ctx, `UPDATE workers SET offline_notified=true
		WHERE enabled AND NOT offline_notified AND last_seen_at IS NOT NULL AND last_seen_at < now() - make_interval(mins => $1)
		RETURNING name`, cfg.WorkerOfflineMinutes)
	if err != nil {
		return err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, n := range names {
		_ = a.Queue.Emit(ctx, jobs.Event{Type: "worker.offline", Title: "Processing worker \"" + n + "\" is offline",
			Body: "Documents waiting for text recognition will be delayed until it's back, or until a fallback engine takes over.",
			Link: "/admin/processing", Severity: "warning", ToAdmins: true})
	}
	return nil
}

// storageAlert warns admins at most once a day when free space is low.
func (w *MaintenanceWorker) storageAlert(ctx context.Context) {
	a := w.A
	free := a.Store.FreeBytes()
	if free < 0 || free > 5<<30 {
		return
	}
	var last time.Time
	if ok, _ := a.Settings.Get(ctx, "maintenance.storage_alert_at", &last); ok && time.Since(last) < 24*time.Hour {
		return
	}
	_ = a.Settings.Set(ctx, "maintenance.storage_alert_at", time.Now(), uuid.Nil)
	_ = a.Queue.Emit(ctx, jobs.Event{Type: "storage.low", Title: "Docveta is running low on disk space",
		Body: "Less than 5 GB is free where documents are stored. Free up space or add storage soon.", Link: "/admin/system",
		Severity: "warning", ToAdmins: true})
}

// blobGCGrace keeps unreferenced blobs for a while so backups (DB first, then blobs)
// are always consistent and in-flight uploads are never collected (DESIGN §19.1).
const blobGCGrace = 7 * 24 * time.Hour

func (w *MaintenanceWorker) gcBlobs(ctx context.Context) error {
	a := w.A
	var last time.Time
	if ok, _ := a.Settings.Get(ctx, "maintenance.blob_gc_at", &last); ok && time.Since(last) < 24*time.Hour {
		return nil
	}
	refs, err := ReferencedBlobs(ctx, a.Pool)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-blobGCGrace)
	removed := 0
	err = a.Store.Walk(ctx, func(key string, mod time.Time) error {
		if _, ok := refs[key]; ok || mod.After(cutoff) {
			return nil
		}
		if err := a.Store.Delete(ctx, key); err != nil {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return err
	}
	// Old OCR task results of superseded runs are no longer needed once a newer run is current.
	if _, err := a.Pool.Exec(ctx, `UPDATE processing_tasks SET result_blob_key=NULL
		WHERE result_blob_key IS NOT NULL AND finished_at < now() - interval '30 days'
		  AND ocr_run_id IN (SELECT id FROM ocr_runs WHERE NOT is_current)`); err != nil {
		return err
	}
	if _, err := a.Pool.Exec(ctx, `DELETE FROM processing_tasks WHERE status IN ('done','cancelled') AND finished_at < now() - interval '90 days'
		AND ocr_run_id IN (SELECT id FROM ocr_runs WHERE NOT is_current)`); err != nil {
		return err
	}
	if removed > 0 {
		a.Log.Info("blob gc", "removed", removed)
	}
	return a.Settings.Set(ctx, "maintenance.blob_gc_at", time.Now(), uuid.Nil)
}

// ReferencedBlobs returns every blob key the database still points to (files, OCR
// results). Used by blob GC and by `docveta doctor`.
func ReferencedBlobs(ctx context.Context, pool *pgxpool.Pool) (map[string]struct{}, error) {
	rows, err := pool.Query(ctx, `SELECT blob_key FROM document_files
		UNION SELECT result_blob_key FROM processing_tasks WHERE result_blob_key IS NOT NULL
		UNION SELECT result_blob_key FROM ocr_runs WHERE result_blob_key IS NOT NULL
		UNION SELECT payload->>'ocr_result_key' FROM processing_tasks WHERE payload ? 'ocr_result_key'`)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	refs := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		refs[k] = struct{}{}
	}
	return refs, nil
}
