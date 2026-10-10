package app

// End-to-end test of the HTTP API, background jobs and the worker protocol against a
// real PostgreSQL. It runs in a throwaway schema, so it never touches existing data:
//
//	DOCVETA_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/docveta_test go test ./internal/app -run Integration -v

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/api"
	"github.com/anand34577/docveta/internal/platform/config"
)

const enrollKey = "0123456789abcdef0123456789abcdef"

// env is a running Docveta (database schema, jobs, HTTP server) with a signed-in admin.
type env struct {
	ctx    context.Context
	a      *App
	srv    *httptest.Server
	c      *client // signed in as the admin
	family string  // id of the shared "Family" space
	me     string  // admin user id
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

// newEnvWith is newEnv with a chance to adjust the configuration (storage, limits, ...).
func newEnvWith(t *testing.T, adjust func(*config.Config)) *env {
	t.Helper()
	dbURL := os.Getenv("DOCVETA_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("set DOCVETA_TEST_DATABASE_URL to run integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	// Isolated schema per run.
	schema := fmt.Sprintf("docveta_it_%d", time.Now().UnixNano())
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	// Extensions live in public so that several environments in one test share them.
	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS citext SCHEMA public; CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		conn.Close(context.Background())
	})
	u, _ := url.Parse(dbURL)
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()

	srv := httptest.NewUnstartedServer(nil)
	base, _ := url.Parse("http://" + srv.Listener.Addr().String())
	cfg := &config.Config{
		BaseURL: base, DatabaseURL: u.String(), DataDir: t.TempDir(), SecretKey: []byte(strings.Repeat("s", 32)),
		MaxUploadBytes: 50 << 20, SessionIdle: time.Hour, SessionMax: time.Hour, TrashRetention: time.Hour,
		PDFWorkers: 1, JobWorkers: 4, DevMode: true, WorkerEnrollKey: []byte(enrollKey),
	}
	if adjust != nil {
		adjust(cfg)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	a, err := New(ctx, cfg, log, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if err := a.startJobs(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.River.Stop(context.Background()) }) //nolint:errcheck
	srv.Config.Handler = a.Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	specRouter = loadSpec(t, srv.URL)

	c := newClient(t, srv.URL)

	// --- Setup & session ---------------------------------------------------
	c.do("POST", "/api/v1/setup", map[string]any{"email": "admin@example.com", "display_name": "Admin User",
		"password": "a-long-password", "shared_space_name": "Family"}, 201, nil)
	var me struct {
		ID     string                            `json:"id"`
		Spaces []struct{ ID, Name, Kind string } `json:"spaces"`
	}
	c.do("GET", "/api/v1/me", nil, 200, &me)
	if len(me.Spaces) != 2 {
		t.Fatalf("expected personal + shared space, got %+v", me.Spaces)
	}
	family := me.Spaces[1].ID
	if me.Spaces[0].Kind != "personal" {
		family = me.Spaces[0].ID
	}
	// Setup can't run twice.
	c.do("POST", "/api/v1/setup", map[string]any{"email": "x@example.com", "display_name": "X", "password": "a-long-password"}, 409, nil)
	return &env{ctx: ctx, a: a, srv: srv, c: c, family: family, me: me.ID}
}

func TestIntegration(t *testing.T) {
	ev := newEnv(t)
	ctx, a, srv, c, family := ev.ctx, ev.a, ev.srv, ev.c, ev.family

	// Auto-tag rule: documents mentioning "invoice" get the tag.
	var tag struct{ ID string }
	c.do("POST", "/api/v1/tags", map[string]any{"space_id": family, "name": "Invoices", "color": "blue",
		"match_algorithm": "any", "match_pattern": "invoice"}, 201, &tag)

	// --- Born-digital PDF: indexed without any worker ----------------------
	doc := c.upload(family, "bescom_bill.pdf", minimalPDF("Electricity bill from BESCOM, invoice 05/08/2026"))
	doc = c.waitStatus(doc.ID, "ready")
	if doc.PageCount == nil || *doc.PageCount != 1 || !doc.HasThumbnail {
		t.Errorf("preprocess incomplete: %+v", doc)
	}
	if doc.DocumentDate == nil || *doc.DocumentDate != "2026-08-05" {
		t.Errorf("date not extracted (DD/MM): %v", doc.DocumentDate)
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "Invoices" {
		t.Errorf("auto-tag rule not applied: %+v", doc.Tags)
	}
	var list struct {
		Items []docDTO `json:"items"`
		Total *int     `json:"total"`
	}
	c.do("GET", "/api/v1/documents?q=bescom", nil, 200, &list)
	if len(list.Items) != 1 || len(list.Items[0].Snippet) == 0 {
		t.Fatalf("search failed: %+v", list)
	}
	c.do("GET", "/api/v1/documents?q=electr", nil, 200, &list) // prefix
	if len(list.Items) != 1 {
		t.Fatalf("prefix search failed")
	}
	c.do("GET", "/api/v1/documents?q=tag:invoices", nil, 200, &list)
	if len(list.Items) != 1 {
		t.Fatalf("tag filter failed")
	}

	// Duplicate upload is detected.
	c.uploadStatus(family, "again.pdf", minimalPDF("Electricity bill from BESCOM, invoice 05/08/2026"), 409)

	// Optimistic concurrency.
	c.doH("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"title": "x"}, map[string]string{"If-Match": `"999"`}, 412, nil)
	c.doH("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"title": "BESCOM August"}, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, doc.Version)}, 200, nil)

	// --- Image: needs OCR from a worker ------------------------------------
	var wk struct {
		Token string `json:"token"`
	}
	c.do("POST", "/api/v1/admin/workers", map[string]any{"name": "test-npu"}, 201, &wk)
	w := newClient(t, srv.URL)
	w.token = wk.Token
	w.do("POST", "/worker/v1/hello", map[string]any{
		"protocol": []int{1},
		"worker":   map[string]any{"name": "test-npu", "version": "1"},
		"capabilities": []map[string]any{
			{"task_type": "ocr", "engine": "fake", "languages": []string{"en"}, "input_mime": []string{"image/*", "application/pdf"}, "concurrency": 2, "tags": []string{"npu"}},
			{"task_type": "archive", "engine": "docveta-textlayer", "concurrency": 1},
		},
	}, 200, nil)

	// Enrollment (Docker): a wrong key is refused; the right one gives a working token,
	// and enrolling again replaces it.
	e := newClient(t, srv.URL)
	e.do("POST", "/worker/v1/enroll", map[string]any{"name": "docker-ocr", "key": "wrong"}, 401, nil)
	var en, en2 struct {
		Token string `json:"token"`
	}
	e.do("POST", "/worker/v1/enroll", map[string]any{"name": "docker-ocr", "key": enrollKey}, 200, &en)
	e.do("POST", "/worker/v1/enroll", map[string]any{"name": "docker-ocr", "key": enrollKey}, 200, &en2)
	archiveOnly := map[string]any{"protocol": []int{1}, "worker": map[string]any{"name": "docker-ocr", "version": "1"},
		"capabilities": []map[string]any{{"task_type": "archive", "engine": "docveta-textlayer", "concurrency": 1}}}
	e.token = en.Token
	e.do("POST", "/worker/v1/hello", archiveOnly, 401, nil)
	e.token = en2.Token
	e.do("POST", "/worker/v1/hello", archiveOnly, 200, nil)

	img := c.upload(family, "scan.png", pngBytes())
	img = c.waitStage(img.ID, "awaiting_ocr")

	var leased struct {
		Tasks []struct {
			TaskID   string `json:"task_id"`
			LeaseID  string `json:"lease_id"`
			Type     string `json:"type"`
			Input    struct{ URL, Mime string }
			Complete string `json:"complete_url"`
		} `json:"tasks"`
	}
	w.do("POST", "/worker/v1/lease", map[string]any{"capacity": 1, "wait_seconds": 5, "types": []string{"ocr"}}, 200, &leased)
	if len(leased.Tasks) != 1 || leased.Tasks[0].Type != "ocr" {
		t.Fatalf("lease: %+v", leased)
	}
	task := leased.Tasks[0]
	body := w.raw("GET", task.Input.URL+"?lease_id="+task.LeaseID, nil, nil, 200)
	if !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatal("input download is not the original")
	}
	w.do("POST", "/worker/v1/tasks/"+task.TaskID+"/heartbeat", map[string]any{"lease_id": task.LeaseID, "progress": map[string]any{"pages_done": 1}}, 200, nil)
	var reading struct {
		Progress *struct {
			PagesDone  int `json:"pages_done"`
			PagesTotal int `json:"pages_total"`
		} `json:"progress"`
	}
	c.do("GET", "/api/v1/documents/"+img.ID, nil, 200, &reading)
	if p := reading.Progress; p == nil || p.PagesDone != 1 || p.PagesTotal != 0 { // a picture: only the worker knows its pages
		t.Fatalf("progress while reading: %+v", p)
	}
	// A stale lease is rejected.
	w.do("POST", "/worker/v1/tasks/"+task.TaskID+"/heartbeat", map[string]any{"lease_id": "00000000-0000-0000-0000-000000000000"}, 409, nil)

	result := map[string]any{"schema": "ocr-result/v1", "engine": map[string]any{"name": "fake", "version": "1"},
		"pages": []map[string]any{{"page": 1, "width": 200, "height": 100, "dpi": 72, "rotation": 0, "confidence": 0.97,
			"text": "Docveta test invoice number 42",
			"blocks": []map[string]any{{"lines": []map[string]any{{"text": "Docveta test invoice number 42", "bbox": []float64{10, 10, 190, 30},
				"words": []map[string]any{{"text": "Docveta", "bbox": []float64{10, 10, 50, 30}}, {"text": "invoice", "bbox": []float64{90, 10, 140, 30}}}}}}}}}}
	w.completeMultipart(task.Complete, task.LeaseID, "result", "result.json", mustJSON(result), 204)
	img = c.waitStatus(img.ID, "ready")
	if len(img.Tags) != 1 {
		t.Errorf("rule not applied after OCR: %+v", img.Tags)
	}
	c.do("GET", "/api/v1/documents?q=invoice+number", nil, 200, &list)
	found := false
	for _, d := range list.Items {
		found = found || d.ID == img.ID
	}
	if !found {
		t.Fatal("OCR text not searchable")
	}

	// Archive (searchable PDF) task follows because the result had word boxes.
	w.do("POST", "/worker/v1/lease", map[string]any{"capacity": 1, "wait_seconds": 5, "types": []string{"archive"}}, 200, &leased)
	if len(leased.Tasks) != 1 || leased.Tasks[0].Type != "archive" {
		t.Fatalf("archive lease: %+v", leased)
	}
	at := leased.Tasks[0]
	w.completeMultipart(at.Complete, at.LeaseID, "archive", "archive.pdf", minimalPDF("archive"), 204)
	c.do("GET", "/api/v1/documents/"+img.ID, nil, 200, &img)
	if !img.HasArchive {
		t.Fatal("archive not attached")
	}
	c.raw("GET", "/api/v1/documents/"+img.ID+"/file?kind=archive", nil, nil, 200)

	// --- Trash, restore, permissions ---------------------------------------
	c.do("DELETE", "/api/v1/documents/"+doc.ID, nil, 204, nil)
	c.do("GET", "/api/v1/documents?q=bescom", nil, 200, &list)
	if len(list.Items) != 0 {
		t.Fatal("trashed document still listed")
	}
	c.do("POST", "/api/v1/documents/"+doc.ID+"/restore", nil, 204, nil)

	// A second user can't see the family space until added.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	k := newClient(t, srv.URL)
	k.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	k.do("GET", "/api/v1/documents/"+doc.ID, nil, 404, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	var kidID string
	for _, d := range dir.Items {
		if d.Email == "kid@example.com" {
			kidID = d.ID
		}
	}
	c.do("PUT", "/api/v1/spaces/"+family+"/members/"+kidID, map[string]any{"role": "viewer"}, 204, nil)
	k.do("GET", "/api/v1/documents/"+doc.ID, nil, 200, nil)
	k.do("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"title": "nope"}, 403, nil)
	k.do("POST", "/api/v1/admin/workers", map[string]any{"name": "x"}, 403, nil)

	// CSRF: a cross-site POST with the session cookie is refused.
	c.doH("POST", "/api/v1/documents/"+doc.ID+"/notes", map[string]any{"body": "hi"}, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403, nil)
	c.do("POST", "/api/v1/documents/"+doc.ID+"/notes", map[string]any{"body": "Paid on time"}, 201, nil)

	// --- Read endpoints (responses are validated against openapi.yaml) -------
	for _, p := range []string{
		"/api/v1/status", "/api/v1/openapi.yaml", "/api/v1/me/sessions", "/api/v1/me/tokens", "/api/v1/spaces",
		"/api/v1/spaces/" + family, "/api/v1/spaces/" + family + "/members", "/api/v1/tags?space_id=" + family,
		"/api/v1/tags/" + tag.ID, "/api/v1/correspondents", "/api/v1/document-types", "/api/v1/documents/stats",
		"/api/v1/documents/suggest?q=bes", "/api/v1/documents/" + doc.ID + "/pages", "/api/v1/documents/" + doc.ID + "/notes",
		"/api/v1/documents/" + doc.ID + "/history", "/api/v1/documents/" + doc.ID + "/thumbnail",
		"/api/v1/documents/" + doc.ID + "/file?kind=best", "/api/v1/notifications", "/api/v1/notification-channels",
		"/api/v1/changes", "/api/v1/changes?since=999999999", "/api/v1/saved-views", "/api/v1/admin/users",
		"/api/v1/admin/settings/oidc", "/api/v1/admin/settings/smtp", "/api/v1/admin/settings/processing",
		"/api/v1/admin/workers", "/api/v1/admin/tasks", "/api/v1/admin/system", "/api/v1/admin/audit",
		"/api/v1/documents?trash=true&sort=-added",
	} {
		c.raw("GET", p, nil, nil, 200)
	}
	var view struct{ ID string }
	c.do("POST", "/api/v1/saved-views", map[string]any{"name": "Bills", "query": map[string]any{"q": "bill"}}, 201, &view)
	c.do("PATCH", "/api/v1/saved-views/"+view.ID, map[string]any{"name": "Electricity bills"}, 200, nil)
	c.do("POST", "/api/v1/documents/search", map[string]any{"q": "bescom"}, 200, nil)
	c.do("POST", "/api/v1/documents/bulk", map[string]any{"action": "update", "ids": []string{doc.ID}, "update": map[string]any{"inbox": false}}, 200, nil)
	c.do("DELETE", "/api/v1/saved-views/"+view.ID, nil, 204, nil)

	// --- API token scopes --------------------------------------------------
	tokenFor := func(scopes ...string) *client {
		var res struct{ Secret string }
		c.do("POST", "/api/v1/me/tokens", map[string]any{"name": strings.Join(scopes, "+"), "scopes": scopes}, 201, &res)
		tc := newClient(t, srv.URL)
		tc.token = res.Secret
		return tc
	}
	ro := tokenFor("documents:read")
	ro.do("GET", "/api/v1/documents/"+doc.ID, nil, 200, nil)
	ro.uploadStatus(family, "ro.pdf", minimalPDF("read only token upload"), 403)
	ro.do("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"title": "nope"}, 403, nil)
	up := tokenFor("upload")
	up.upload(family, "up.pdf", minimalPDF("upload only token"))
	up.do("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"title": "nope"}, 403, nil)
	up.do("DELETE", "/api/v1/documents/"+doc.ID+"?permanent=true", nil, 403, nil)
	up.do("POST", "/api/v1/me/tokens", map[string]any{"name": "x", "scopes": []string{"upload"}}, 403, nil)

	// --- New-sign-in notification only for unseen devices --------------------
	countLogins := func() int {
		var n int
		if err := a.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications n JOIN users u ON u.id=n.user_id
			WHERE u.email='kid@example.com' AND n.event_type='security.new_login'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	k2 := newClient(t, srv.URL)
	k2.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	time.Sleep(time.Second) // notifications are delivered by a background job
	if n := countLogins(); n != 0 {
		t.Errorf("same device signed in again: %d new-login notifications, want 0", n)
	}
	k3 := newClient(t, srv.URL)
	k3.doH("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"},
		map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 15)"}, 200, nil)
	deadline := time.Now().Add(10 * time.Second)
	for countLogins() != 1 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if n := countLogins(); n != 1 {
		t.Errorf("new device: %d new-login notifications, want 1", n)
	}

	// --- Lease expiry → reaper → re-queue → attempts exhausted → failed → admin retry
	lost := c.upload(family, "lost.png", pngBytesY(70))
	c.waitStage(lost.ID, "awaiting_ocr")
	var taskID string
	leaseOCR := func() {
		t.Helper()
		var l struct {
			Tasks []struct {
				TaskID     string `json:"task_id"`
				DocumentID string `json:"document_id"`
			} `json:"tasks"`
		}
		// Other test documents may have OCR tasks queued too; take ours.
		w.do("POST", "/worker/v1/lease", map[string]any{"capacity": 10, "wait_seconds": 5, "types": []string{"ocr"}}, 200, &l)
		for _, lt := range l.Tasks {
			if lt.DocumentID == lost.ID {
				taskID = lt.TaskID
				return
			}
		}
		t.Fatalf("lease: no task for %s in %+v", lost.ID, l)
	}
	expire := func() { // the worker "dies": its lease runs out and the reaper runs
		t.Helper()
		if _, err := a.Pool.Exec(ctx, `UPDATE processing_tasks SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, taskID); err != nil {
			t.Fatal(err)
		}
		if err := a.Pipeline.ReapLeases(ctx); err != nil {
			t.Fatal(err)
		}
	}
	taskStatus := func() (st string) {
		_ = a.Pool.QueryRow(ctx, `SELECT status FROM processing_tasks WHERE id=$1`, taskID).Scan(&st)
		return st
	}
	leaseOCR()
	expire()
	if st := taskStatus(); st != "queued" {
		t.Fatalf("expired lease: task %s, want queued", st)
	}
	_, _ = a.Pool.Exec(ctx, `UPDATE processing_tasks SET not_before=now(), max_attempts=2 WHERE id=$1`, taskID) // skip back-off
	leaseOCR()
	expire()
	if st := taskStatus(); st != "failed" {
		t.Fatalf("attempts exhausted: task %s, want failed", st)
	}
	deadline = time.Now().Add(20 * time.Second)
	var ld docDTO
	for ; time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if c.do("GET", "/api/v1/documents/"+lost.ID, nil, 200, &ld); ld.Status == "failed" {
			break
		}
	}
	if ld.Status != "failed" {
		t.Fatalf("document after OCR failure: %+v, want failed", ld)
	}
	c.do("POST", "/api/v1/admin/tasks/"+taskID+"/retry", nil, 204, nil)
	c.do("POST", "/api/v1/admin/tasks/"+taskID+"/retry", nil, 409, nil) // only failed tasks
	var rl struct {
		Tasks []struct {
			TaskID   string `json:"task_id"`
			LeaseID  string `json:"lease_id"`
			Complete string `json:"complete_url"`
		} `json:"tasks"`
	}
	w.do("POST", "/worker/v1/lease", map[string]any{"capacity": 10, "wait_seconds": 5, "types": []string{"ocr"}}, 200, &rl)
	retried := -1
	for i, lt := range rl.Tasks {
		if lt.TaskID == taskID {
			retried = i
		}
	}
	if retried < 0 {
		t.Fatalf("retried task not leased again: %+v", rl)
	}
	w.completeMultipart(rl.Tasks[retried].Complete, rl.Tasks[retried].LeaseID, "result", "result.json", mustJSON(result), 204)
	if d := c.waitStatus(lost.ID, "ready"); d.Error != "" {
		t.Errorf("error not cleared after successful retry: %q", d.Error)
	}

	// --- SSRF guard: only administrators' channels reach the local network by default ---
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer hook.Close()
	newHook := func(cl *client) string {
		var ch struct{ ID string }
		cl.do("POST", "/api/v1/notification-channels", map[string]any{"name": "Local hook", "type": "webhook",
			"config": map[string]any{"url": hook.URL}, "events": []string{"document.processed"}}, 201, &ch)
		return ch.ID
	}
	kidHook := newHook(k2)
	b := k2.raw("POST", "/api/v1/notification-channels/"+kidHook+"/test", nil, nil, 422)
	if !strings.Contains(string(b), "local network") {
		t.Errorf("a member's local webhook target wasn't blocked: %s", b)
	}
	// An administrator's own Gotify/ntfy/webhook at home works.
	c.raw("POST", "/api/v1/notification-channels/"+newHook(c)+"/test", nil, nil, 204)
	// And everyone's, once an administrator allows it.
	c.do("PUT", "/api/v1/admin/settings/server", map[string]any{"public_url": "", "allow_local_targets": true}, 200, nil)
	k2.raw("POST", "/api/v1/notification-channels/"+kidHook+"/test", nil, nil, 204)
	c.do("PUT", "/api/v1/admin/settings/server", map[string]any{"public_url": "", "allow_local_targets": false}, 200, nil)
	k2.do("PUT", "/api/v1/admin/settings/server", map[string]any{"allow_local_targets": true}, 403, nil)
}

// ---------------------------------------------------------------------------

type docDTO struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Stage        string `json:"processing_stage"`
	Error        string `json:"processing_error"`
	PageCount    *int   `json:"page_count"`
	HasThumbnail bool   `json:"has_thumbnail"`
	HasArchive   bool   `json:"has_archive"`
	HasDerived   bool   `json:"has_derived"`
	Title        string `json:"title"`
	Inbox        bool   `json:"inbox"`
	Space        struct {
		ID string `json:"id"`
	} `json:"space"`
	Source       string  `json:"source"`
	DocumentDate *string `json:"document_date"`
	Version      int     `json:"version"`
	Tags         []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Snippet []any `json:"snippet"`
}

type client struct {
	t     *testing.T
	base  string
	h     *http.Client
	token string
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, h: &http.Client{Jar: jar, Timeout: 60 * time.Second}}
}

func (c *client) raw(method, path string, body io.Reader, headers map[string]string, want int) []byte {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.h.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		c.t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, want, b)
	}
	validateResponse(c.t, req, res, b)
	return b
}

// specRouter matches requests to operations in api/openapi.yaml; every JSON response
// the test sees is validated against the spec.
var specRouter routers.Router

func loadSpec(t *testing.T, serverURL string) routers.Router {
	doc, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	doc.Servers = openapi3.Servers{{URL: serverURL}}
	r, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func validateResponse(t *testing.T, req *http.Request, res *http.Response, body []byte) {
	t.Helper()
	if specRouter == nil {
		return
	}
	route, params, err := specRouter.FindRoute(req)
	if err != nil {
		t.Errorf("%s %s: not in openapi.yaml: %v", req.Method, req.URL.Path, err)
		return
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 res.StatusCode, Header: res.Header, Body: io.NopCloser(bytes.NewReader(body)),
		Options: &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "json") {
		in.Options.ExcludeResponseBody = true // files, event streams
	}
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Errorf("%s %s → %d doesn't match openapi.yaml: %v", req.Method, req.URL.Path, res.StatusCode, err)
	}
}

func (c *client) doH(method, path string, in any, headers map[string]string, want int, out any) {
	c.t.Helper()
	var body io.Reader
	if headers == nil {
		headers = map[string]string{}
	}
	if in != nil {
		body = bytes.NewReader(mustJSON(in))
		headers["Content-Type"] = "application/json"
	}
	b := c.raw(method, path, body, headers, want)
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("decode %s: %v: %s", path, err, b)
		}
	}
}

func (c *client) do(method, path string, in any, want int, out any) {
	c.t.Helper()
	c.doH(method, path, in, nil, want, out)
}

func (c *client) uploadStatus(space, name string, data []byte, want int) []byte {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("space_id", space)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return c.raw("POST", "/api/v1/documents", &buf, map[string]string{"Content-Type": mw.FormDataContentType()}, want)
}

func (c *client) upload(space, name string, data []byte) docDTO {
	c.t.Helper()
	var d docDTO
	if err := json.Unmarshal(c.uploadStatus(space, name, data, 201), &d); err != nil {
		c.t.Fatal(err)
	}
	return d
}

func (c *client) completeMultipart(path, lease, field, filename string, data []byte, want int) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("lease_id", lease)
	_ = mw.WriteField("metrics", `{"ms_total":5}`)
	fw, _ := mw.CreateFormFile(field, filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	c.raw("POST", path, &buf, map[string]string{"Content-Type": mw.FormDataContentType()}, want)
}

func (c *client) wait(id string, ok func(docDTO) bool) docDTO {
	c.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var d docDTO
	for time.Now().Before(deadline) {
		d = docDTO{} // omitempty fields (processing_error) vanish when cleared
		c.do("GET", "/api/v1/documents/"+id, nil, 200, &d)
		if ok(d) {
			return d
		}
		if d.Status == "failed" {
			c.t.Fatalf("document failed: %s", d.Error)
		}
		time.Sleep(300 * time.Millisecond)
	}
	c.t.Fatalf("timeout waiting for document %s: %+v", id, d)
	return d
}

func (c *client) waitStatus(id, status string) docDTO {
	return c.wait(id, func(d docDTO) bool { return d.Status == status })
}

func (c *client) waitStage(id, stage string) docDTO {
	return c.wait(id, func(d docDTO) bool { return d.Stage == stage })
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func pngBytes() []byte { return pngBytesY(50) }

// pngBytesY draws a line at height y, so different y values give different files.
func pngBytesY(y int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for x := 0; x < 200; x++ {
		img.Set(x, y, color.Black)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// minimalPDF builds a one-page born-digital PDF with the given text.
func minimalPDF(text string) []byte {
	var b bytes.Buffer
	var off []int
	obj := func(s string) {
		off = append(off, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(off), s)
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [4 0 R] /Count 1 >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents 5 0 R >>")
	stream := fmt.Sprintf("BT /F1 14 Tf 72 760 Td (%s) Tj ET", text)
	obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(off)+1)
	for _, o := range off {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(off)+1, x)
	return b.Bytes()
}

func newMultipart(w io.Writer) *multipart.Writer { return multipart.NewWriter(w) }
