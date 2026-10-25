package app

import (
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
