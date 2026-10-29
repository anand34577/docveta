package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Clean-up of old task results keeps those of a failed run that can still be retried, and
// drops them once a newer run has replaced it.
func TestBlobGCKeepsRetryableRuns(t *testing.T) {
	e := newEnv(t)
	ctx, a := e.ctx, e.a
	doc := e.c.upload(e.family, "scan.png", pngBytes())
	e.c.waitStage(doc.ID, "awaiting_ocr")
	var task, run string
	if err := a.Pool.QueryRow(ctx, `UPDATE processing_tasks SET status='done', result_blob_key='old-result', finished_at=now() - interval '40 days'
		WHERE document_id=$1 RETURNING id, ocr_run_id`, doc.ID).Scan(&task, &run); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pool.Exec(ctx, `UPDATE ocr_runs SET status='failed', finished_at=now() WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	gc := func() *string {
		t.Helper()
		if err := a.Settings.Set(ctx, "maintenance.blob_gc_at", time.Time{}, uuid.Nil); err != nil {
			t.Fatal(err)
		}
		if err := (&MaintenanceWorker{A: a}).gcBlobs(ctx); err != nil {
			t.Fatal(err)
		}
		var key *string
		if err := a.Pool.QueryRow(ctx, `SELECT result_blob_key FROM processing_tasks WHERE id=$1`, task).Scan(&key); err != nil {
			t.Fatal(err)
		}
		return key
	}
	if key := gc(); key == nil {
		t.Fatal("result of the latest (failed, retryable) run was dropped")
	}
	if _, err := a.Pool.Exec(ctx, `INSERT INTO ocr_runs (id, document_id, version_no, status) VALUES ($1, $2, 1, 'running')`,
		uuid.Must(uuid.NewV7()), doc.ID); err != nil {
		t.Fatal(err)
	}
	if key := gc(); key != nil {
		t.Fatalf("result of a replaced run kept: %s", *key)
	}
}

// Clean-up of stored files removes old ones nothing points to and keeps every file in use.
func TestBlobGCRemovesOnlyUnused(t *testing.T) {
	e := newEnv(t)
	ctx, a := e.ctx, e.a
	doc := e.c.upload(e.family, "kept.pdf", minimalPDF("A paper that stays"))
	st, err := a.FS.Stage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Write([]byte("nothing points to this")); err != nil {
		t.Fatal(err)
	}
	orphan, err := st.Commit()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := filepath.WalkDir(a.Cfg.DataDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".bin") {
			err = os.Chtimes(p, old, old)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var kept []string
	rows, err := a.Pool.Query(ctx, `SELECT blob_key FROM document_files WHERE document_id=$1`, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, k)
	}
	if len(kept) == 0 {
		t.Fatal("the upload has no stored files")
	}
	if err := a.Settings.Set(ctx, "maintenance.blob_gc_at", time.Time{}, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	if err := (&MaintenanceWorker{A: a}).gcBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.Store.Exists(ctx, orphan); ok {
		t.Error("an old file nothing points to was kept")
	}
	for _, k := range kept {
		if ok, _ := a.Store.Exists(ctx, k); !ok {
			t.Errorf("file in use removed: %s", k)
		}
	}
}

// Notifications older than half a year are removed; newer ones stay.
func TestPruneNotifications(t *testing.T) {
	e := newEnv(t)
	ctx, a := e.ctx, e.a
	for i, age := range []string{"200 days", "181 days", "10 days"} {
		if _, err := a.Pool.Exec(ctx, `INSERT INTO notifications (id, user_id, event_type, title, created_at)
			VALUES ($1, $2, 'test.event', 'n', now() - $3::interval)`, uuid.Must(uuid.NewV7()), e.me, age); err != nil {
			t.Fatal(i, err)
		}
	}
	n, err := pruneNotifications(ctx, a.Pool)
	if err != nil || n != 2 {
		t.Fatalf("removed %d, %v; want 2", n, err)
	}
	var left int
	if err := a.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND event_type='test.event'`, e.me).Scan(&left); err != nil || left != 1 {
		t.Fatalf("%d left, %v", left, err)
	}
}
