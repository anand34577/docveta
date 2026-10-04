package app

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gen2brain/avif"
	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/barcode"
	"github.com/anand34577/docveta/internal/exchange"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/config"
)

// jsonList reads the "items" of a list response.
type jsonList[T any] struct {
	Items []T `json:"items"`
}

func (e *env) list(path string) []docDTO {
	var l jsonList[docDTO]
	e.c.do("GET", path, nil, 200, &l)
	return l.Items
}

func (c *client) uploadVersion(id, name string, data []byte) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := newMultipart(&buf)
	_ = mw.WriteField("note", "Replaced in test")
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	c.raw("POST", "/api/v1/documents/"+id+"/versions", &buf, map[string]string{"Content-Type": mw.FormDataContentType()}, 201)
}

// TestDocumentEditing covers unlock, page edits, merge, split, versions and the other
// document-level features added with them.
func TestDocumentEditing(t *testing.T) {
	e := newEnv(t)
	c, family := e.c, e.family

	// --- Unlock a password-protected PDF -------------------------------------
	enc, err := os.ReadFile("../pipeline/testdata/encrypted.pdf") // password: hunter2-test
	if err != nil {
		t.Fatal(err)
	}
	locked := c.upload(family, "statement.pdf", enc)
	locked = c.waitStatus(locked.ID, "needs_password")
	c.do("POST", "/api/v1/documents/"+locked.ID+"/unlock", map[string]any{"password": "wrong"}, 422, nil)
	c.do("POST", "/api/v1/documents/"+locked.ID+"/unlock", map[string]any{"password": ""}, 422, nil)
	c.do("POST", "/api/v1/documents/"+locked.ID+"/unlock", map[string]any{"password": "hunter2-test"}, 200, nil)
	locked = c.waitStatus(locked.ID, "ready")
	if got := e.list("/api/v1/documents?q=secret+bank+statement"); len(got) != 1 || got[0].ID != locked.ID {
		t.Fatalf("unlocked document isn't searchable: %+v", got)
	}
	c.do("POST", "/api/v1/documents/"+locked.ID+"/unlock", map[string]any{"password": "x"}, 409, nil) // no longer locked
	var versions jsonList[struct {
		No      int    `json:"version_no"`
		Note    string `json:"note"`
		Current bool   `json:"current"`
	}]
	c.do("GET", "/api/v1/documents/"+locked.ID+"/versions", nil, 200, &versions)
	if len(versions.Items) != 2 || !versions.Items[0].Current || versions.Items[1].No != 1 {
		t.Fatalf("versions after unlock: %+v", versions.Items)
	}
	// The encrypted original is still downloadable as version 1.
	if b := c.raw("GET", "/api/v1/documents/"+locked.ID+"/file?kind=original&version=1", nil, nil, 200); !bytes.Equal(b, enc) {
		t.Error("version 1 is not the encrypted original")
	}

	// --- Merge, edit pages, restore ------------------------------------------
	a := c.upload(family, "a.pdf", minimalPDF("alpha page of the combined test document"))
	b := c.upload(family, "b.pdf", minimalPDF("beta page of the combined test document"))
	c.waitStatus(a.ID, "ready")
	c.waitStatus(b.ID, "ready")
	var merged docDTO
	c.do("POST", "/api/v1/documents/merge", map[string]any{"ids": []string{a.ID, b.ID, a.ID}, "title": "Combined"}, 201, &merged)
	merged = c.waitStatus(merged.ID, "ready")
	if merged.PageCount == nil || *merged.PageCount != 3 {
		t.Fatalf("merged page count: %+v", merged.PageCount)
	}
	c.do("POST", "/api/v1/documents/merge", map[string]any{"ids": []string{a.ID}}, 422, nil)

	c.do("POST", "/api/v1/documents/"+merged.ID+"/pages/edit", map[string]any{"pages": []map[string]any{{"from": 3, "rotate": 90}, {"from": 2}}}, 200, nil)
	c.wait(merged.ID, func(d docDTO) bool { return d.Status == "ready" && d.PageCount != nil && *d.PageCount == 2 })
	c.do("POST", "/api/v1/documents/"+merged.ID+"/pages/edit", map[string]any{"pages": []map[string]any{{"from": 9}}}, 422, nil)
	c.do("POST", "/api/v1/documents/"+merged.ID+"/pages/edit", map[string]any{"pages": []map[string]any{{"from": 1, "rotate": 45}}}, 422, nil)
	c.do("POST", "/api/v1/documents/"+merged.ID+"/pages/edit", map[string]any{"pages": []any{}}, 422, nil)
	c.do("GET", "/api/v1/documents/"+merged.ID+"/versions", nil, 200, &versions)
	if len(versions.Items) != 2 {
		t.Fatalf("versions after edit: %+v", versions.Items)
	}
	// Restore the 3-page original: becomes version 3, current.
	c.do("POST", "/api/v1/documents/"+merged.ID+"/versions/1/restore", nil, 200, nil)
	c.wait(merged.ID, func(d docDTO) bool { return d.Status == "ready" && d.PageCount != nil && *d.PageCount == 3 })
	c.do("POST", "/api/v1/documents/"+merged.ID+"/versions/42/restore", nil, 404, nil)

	// --- Split ---------------------------------------------------------------
	var parts jsonList[docDTO]
	c.do("POST", "/api/v1/documents/"+merged.ID+"/split", map[string]any{"ranges": []string{"1", "2-3"}}, 201, &parts)
	if len(parts.Items) != 2 {
		t.Fatalf("split: %+v", parts.Items)
	}
	p2 := c.waitStatus(parts.Items[1].ID, "ready")
	if p2.PageCount == nil || *p2.PageCount != 2 {
		t.Errorf("part 2 pages: %v", p2.PageCount)
	}
	c.do("POST", "/api/v1/documents/"+merged.ID+"/split", map[string]any{"ranges": []string{"1", "9"}}, 422, nil)
	c.do("POST", "/api/v1/documents/"+merged.ID+"/split", map[string]any{"ranges": []string{"1-3"}}, 422, nil)

	// --- Replace the file with a new version ----------------------------------
	c.uploadVersion(a.ID, "a2.pdf", minimalPDF("alpha second edition of the test document"))
	c.wait(a.ID, func(d docDTO) bool { return d.Status == "ready" && d.Version > a.Version })
	if got := e.list("/api/v1/documents?q=second+edition"); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("new version text not searchable: %+v", got)
	}

	// --- Images: rotate a PNG ---------------------------------------------------
	img := c.upload(family, "photo.png", pngBytesY(20))
	c.waitStage(img.ID, "awaiting_ocr")
	c.do("POST", "/api/v1/documents/"+img.ID+"/pages/edit", map[string]any{"pages": []map[string]any{{"from": 1, "rotate": 90}}}, 200, nil)
	c.do("POST", "/api/v1/documents/"+img.ID+"/split", map[string]any{"ranges": []string{"1", "1"}}, 415, nil) // PDFs only

	// --- Empty trash ------------------------------------------------------------
	c.do("DELETE", "/api/v1/documents/"+a.ID, nil, 204, nil)
	c.do("DELETE", "/api/v1/documents/"+b.ID, nil, 204, nil)
	var res struct{ Purged, Skipped int }
	c.do("DELETE", "/api/v1/trash", nil, 200, &res)
	if res.Purged != 2 {
		t.Errorf("empty trash: %+v", res)
	}
	c.do("GET", "/api/v1/documents/"+b.ID, nil, 404, nil)

	// --- Stats tell the UI whether anything can read scans ------------------------
	var st struct {
		OCR  bool `json:"ocr_available"`
		Days int  `json:"trash_retention_days"`
	}
	c.do("GET", "/api/v1/documents/stats", nil, 200, &st)
	if st.OCR {
		t.Error("no worker is online, but ocr_available is true")
	}
}

// TestBulkTagsByName tags documents from different spaces with one tag name.
func TestBulkTagsByName(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var me struct {
		Spaces []struct{ ID, Kind string } `json:"spaces"`
	}
	c.do("GET", "/api/v1/me", nil, 200, &me)
	var personal string
	for _, s := range me.Spaces {
		if s.Kind == "personal" {
			personal = s.ID
		}
	}
	d1 := c.upload(e.family, "one.pdf", minimalPDF("first tagged document used for bulk tests"))
	d2 := c.upload(personal, "two.pdf", minimalPDF("second tagged document used for bulk tests"))
	var res struct{ Succeeded int }
	c.do("POST", "/api/v1/documents/bulk", map[string]any{"action": "update", "ids": []string{d1.ID, d2.ID},
		"update": map[string]any{"add_tag_names": []string{"Tax"}}}, 200, &res)
	if res.Succeeded != 2 {
		t.Fatalf("bulk by name: %+v", res)
	}
	for _, id := range []string{d1.ID, d2.ID} {
		var d docDTO
		c.do("GET", "/api/v1/documents/"+id, nil, 200, &d)
		if len(d.Tags) != 1 || d.Tags[0].Name != "Tax" {
			t.Errorf("document %s tags: %+v", id, d.Tags)
		}
	}
	c.do("POST", "/api/v1/documents/bulk", map[string]any{"action": "update", "ids": []string{d1.ID, d2.ID},
		"update": map[string]any{"remove_tag_names": []string{"tax"}}}, 200, &res)
	var d docDTO
	c.do("GET", "/api/v1/documents/"+d1.ID, nil, 200, &d)
	if len(d.Tags) != 0 {
		t.Errorf("tag not removed by name: %+v", d.Tags)
	}
}

// TestChangeFeedAndReplay covers the sync fixes: a moved document is removed from the
// old space's change feed, and an SSE reconnect replays missed notifications.
func TestChangeFeedAndReplay(t *testing.T) {
	e := newEnv(t)
	c, family := e.c, e.family

	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	k := newClient(t, e.srv.URL)
	k.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	var kid string
	for _, d := range dir.Items {
		if d.Email == "kid@example.com" {
			kid = d.ID
		}
	}
	c.do("PUT", "/api/v1/spaces/"+e.family+"/members/"+kid, map[string]any{"role": "viewer"}, 204, nil)

	doc := c.upload(family, "moving.pdf", minimalPDF("this document will move to another space"))
	c.waitStatus(doc.ID, "ready")
	type change struct {
		Seq     int64  `json:"seq"`
		Entity  string `json:"entity"`
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	var page struct {
		Changes []change `json:"changes"`
		Next    int64    `json:"next_since"`
	}
	k.do("GET", "/api/v1/changes", nil, 200, &page)
	seen := false
	for _, ch := range page.Changes {
		seen = seen || (ch.Entity == "document" && ch.ID == doc.ID && !ch.Deleted)
	}
	if !seen {
		t.Fatalf("kid should see the document before it moves: %+v", page.Changes)
	}
	since := page.Next

	// Move it into the admin's personal space, which the kid can't see.
	var me struct {
		Spaces []struct{ ID, Kind string } `json:"spaces"`
	}
	c.do("GET", "/api/v1/me", nil, 200, &me)
	var personal string
	for _, s := range me.Spaces {
		if s.Kind == "personal" {
			personal = s.ID
		}
	}
	c.do("PATCH", "/api/v1/documents/"+doc.ID, map[string]any{"space_id": personal}, 200, nil)
	k.do("GET", fmt.Sprintf("/api/v1/changes?since=%d", since), nil, 200, &page)
	removed := false
	for _, ch := range page.Changes {
		removed = removed || (ch.Entity == "document" && ch.ID == doc.ID && ch.Deleted)
	}
	if !removed {
		t.Errorf("kid wasn't told the document left the space: %+v", page.Changes)
	}
	// The admin sees both spaces, so the move is an update, never a delete.
	c.do("GET", fmt.Sprintf("/api/v1/changes?since=%d", since), nil, 200, &page)
	for _, ch := range page.Changes {
		if ch.Entity == "document" && ch.ID == doc.ID && ch.Deleted {
			t.Errorf("admin was told to delete a document that only moved: %+v", ch)
		}
	}

	// --- SSE replay ---------------------------------------------------------------
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-00000000000%d", i+1)
		if _, err := e.a.Pool.Exec(e.ctx, `INSERT INTO notifications (id, user_id, event_type, title, created_at)
			VALUES ($1,$2,'test.event',$3, now() + make_interval(secs => $4))`, ids[i], e.me, fmt.Sprintf("n%d", i+1), i); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", ids[0])
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 4096)
	var got strings.Builder
	for !strings.Contains(got.String(), ids[2]) {
		n, err := res.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(got.String(), ids[1]) || !strings.Contains(got.String(), ids[2]) || strings.Contains(got.String(), ids[0]) {
		t.Errorf("replay should contain the two newer notifications only:\n%s", got.String())
	}
}

// TestModernImages checks AVIF/HEIC handling in the core: converted to JPEG for
// thumbnails, the browser and OCR engines; no worker needed.
func TestModernImages(t *testing.T) {
	e := newEnv(t)
	c := e.c
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := 0; x < 64; x++ {
		for y := 0; y < 48; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := avif.Encode(&buf, img); err != nil {
		t.Skipf("AVIF encoder unavailable: %v", err)
	}
	d := c.upload(e.family, "photo.avif", buf.Bytes())
	d = c.waitStage(d.ID, "awaiting_ocr")
	if !d.HasThumbnail || !d.HasDerived || d.PageCount == nil || *d.PageCount != 1 {
		t.Fatalf("avif not converted: %+v", d)
	}
	jpg := c.raw("GET", "/api/v1/documents/"+d.ID+"/file?kind=derived", nil, nil, 200)
	if !bytes.HasPrefix(jpg, []byte{0xff, 0xd8}) {
		t.Error("derived file is not a JPEG")
	}
}

// TestQuietHours holds non-urgent push messages during a user's quiet hours and lets
// urgent ones through; the in-app notification is never delayed.
func TestQuietHours(t *testing.T) {
	e := newEnv(t)
	c := e.c
	uid, _ := uuid.Parse(e.me)

	// Invalid input is rejected.
	c.do("PUT", "/api/v1/me/notification-prefs", map[string]any{"quiet_enabled": true, "quiet_start": "late", "quiet_end": "07:00"}, 422, nil)
	c.do("PUT", "/api/v1/me/notification-prefs", map[string]any{"quiet_enabled": true, "quiet_start": "08:00", "quiet_end": "08:00"}, 422, nil)

	// A quiet window around "now" in the admin's time zone (Asia/Kolkata by default).
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)
	start, end := now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04")
	var prefs struct {
		Enabled bool   `json:"quiet_enabled"`
		Start   string `json:"quiet_start"`
	}
	c.do("PUT", "/api/v1/me/notification-prefs", map[string]any{"quiet_enabled": true, "quiet_start": start, "quiet_end": end}, 200, &prefs)
	c.do("GET", "/api/v1/me/notification-prefs", nil, 200, &prefs)
	if !prefs.Enabled || prefs.Start != start {
		t.Fatalf("prefs not saved: %+v", prefs)
	}

	// An Apprise channel validates its settings.
	c.do("POST", "/api/v1/notification-channels", map[string]any{"name": "Apprise", "type": "apprise", "config": map[string]any{"url": "nope"}}, 422, nil)
	c.do("POST", "/api/v1/notification-channels", map[string]any{"name": "Apprise", "type": "apprise", "config": map[string]any{"url": "http://apprise.example:8000"}}, 422, nil)
	c.do("POST", "/api/v1/notification-channels", map[string]any{"name": "Apprise", "type": "apprise",
		"config": map[string]any{"url": "http://apprise.example:8000", "key": "docveta"}}, 201, nil)

	deferred := func() (n int) {
		_ = e.a.Pool.QueryRow(e.ctx, `SELECT count(*) FROM river_job WHERE kind='notify' AND args->>'external_only'='true' AND state='scheduled'`).Scan(&n)
		return n
	}
	inApp := func(title string) (n int) {
		_ = e.a.Pool.QueryRow(e.ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND title=$2`, e.me, title).Scan(&n)
		return n
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			if ok() {
				return
			}
		}
		rows, _ := e.a.Pool.Query(e.ctx, `SELECT kind, state, left(args::text, 200) FROM river_job WHERE kind='notify'`)
		for rows != nil && rows.Next() {
			var k, s, a string
			_ = rows.Scan(&k, &s, &a)
			t.Logf("river job: %s %s %s", k, s, a)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	if err := e.a.Queue.Emit(e.ctx, jobs.Event{Type: "document.processed", Title: "calm event", Severity: "success", Recipients: []uuid.UUID{uid}}); err != nil {
		t.Fatal(err)
	}
	waitFor("in-app notification", func() bool { return inApp("calm event") == 1 })
	waitFor("push held until quiet hours end", func() bool { return deferred() == 1 })

	if err := e.a.Queue.Emit(e.ctx, jobs.Event{Type: "document.failed", Title: "urgent event", Severity: "error", Recipients: []uuid.UUID{uid}}); err != nil {
		t.Fatal(err)
	}
	waitFor("urgent in-app notification", func() bool { return inApp("urgent event") == 1 })
	time.Sleep(500 * time.Millisecond)
	if n := deferred(); n != 1 {
		t.Errorf("an urgent event was held back: %d deferred jobs, want 1", n)
	}
}

// TestCustomFields covers definitions, typed values, validation, filtering and sorting.
func TestCustomFields(t *testing.T) {
	e := newEnv(t)
	c, family := e.c, e.family

	type field struct {
		ID       string `json:"id"`
		DataType string `json:"data_type"`
	}
	mk := func(name, typ string, opts map[string]any, want int) (f field) {
		c.do("POST", "/api/v1/custom-fields", map[string]any{"space_id": family, "name": name, "data_type": typ, "options": opts}, want, &f)
		return f
	}
	amount := mk("Amount", "monetary", map[string]any{"currency": "inr"}, 201)
	due := mk("Due date", "date", nil, 201)
	status := mk("Status", "select", map[string]any{"choices": []string{"Open", "Paid"}}, 201)
	paid := mk("Reimbursed", "boolean", nil, 201)
	mk("Amount", "text", nil, 409)                                   // names are unique per space
	mk("Bad<name", "text", nil, 422)                                 // operator characters break search syntax
	mk("Pick", "select", map[string]any{"choices": []string{}}, 422) // a select needs choices
	mk("Mystery", "teleport", nil, 422)

	var fields jsonList[struct {
		Name    string         `json:"name"`
		Options map[string]any `json:"options"`
	}]
	c.do("GET", "/api/v1/custom-fields?space_id="+family, nil, 200, &fields)
	if len(fields.Items) != 4 {
		t.Fatalf("fields: %+v", fields.Items)
	}
	for _, f := range fields.Items {
		if f.Name == "Amount" && f.Options["currency"] != "INR" {
			t.Errorf("currency not normalised: %+v", f.Options)
		}
	}

	var docs []docDTO
	set := func(title string, vals map[string]any, want int) docDTO {
		d := c.upload(family, title+".pdf", minimalPDF("custom field document called "+title))
		c.waitStatus(d.ID, "ready") // so its text is indexed before the searches below
		c.do("PATCH", "/api/v1/documents/"+d.ID, map[string]any{"custom_fields": vals}, want, nil)
		return d
	}
	docs = append(docs, set("small", map[string]any{amount.ID: 900, due.ID: "2026-11-15", status.ID: "Open", paid.ID: false}, 200))
	docs = append(docs, set("medium", map[string]any{amount.ID: "1,24,500.50", due.ID: "2026-12-31", status.ID: "Paid", paid.ID: true}, 200))
	docs = append(docs, set("large", map[string]any{amount.ID: 250000, due.ID: "2027-03-01"}, 200))
	// Validation: wrong types and unknown choices are rejected with the offending field named.
	set("bad1", map[string]any{amount.ID: "lots"}, 422)
	set("bad2", map[string]any{status.ID: "Maybe"}, 422)
	set("bad3", map[string]any{due.ID: "31/12/2026"}, 422)
	set("bad4", map[string]any{"not-a-uuid": 1}, 422)

	// Values round-trip with their types.
	var got struct {
		Fields []struct {
			Name     string `json:"name"`
			Value    any    `json:"value"`
			Currency string `json:"currency"`
		} `json:"custom_fields"`
	}
	c.do("GET", "/api/v1/documents/"+docs[1].ID, nil, 200, &got)
	byName := map[string]any{}
	for _, f := range got.Fields {
		byName[f.Name] = f.Value
	}
	if byName["Amount"] != 124500.5 || byName["Due date"] != "2026-12-31" || byName["Status"] != "Paid" || byName["Reimbursed"] != true {
		t.Errorf("values: %+v", byName)
	}

	// Filtering: inline syntax and the structured parameter.
	titles := func(path string) []string {
		var out []string
		for _, d := range e.list(path) {
			out = append(out, d.Title)
		}
		return out
	}
	eq := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	// (Titles come back in the sort order requested; "added" is stable for ties.)
	eq("amount > 1500", titles("/api/v1/documents?q=amount:>1500&sort=title"), "large", "medium")
	eq("cf: syntax", titles("/api/v1/documents?q="+urlEscape(`cf:"Due date"<2026-12-31`)+"&sort=title"), "small")
	eq("select equals", titles("/api/v1/documents?q=status:Paid"), "medium")
	eq("bool", titles("/api/v1/documents?q=reimbursed:yes"), "medium")
	eq("year of date", titles("/api/v1/documents?q="+urlEscape(`cf:"Due date"=2026`)+"&sort=title"), "medium", "small")
	eq("has a value", titles("/api/v1/documents?q=cf:Status&sort=title"), "medium", "small")
	eq("no match", titles("/api/v1/documents?q=amount:>99999999"))

	var res struct {
		Items []docDTO `json:"items"`
	}
	c.do("POST", "/api/v1/documents/search", map[string]any{"custom": []map[string]any{{"name": "Amount", "op": "<", "value": "1000"}}}, 200, &res)
	if len(res.Items) != 1 || res.Items[0].Title != "small" {
		t.Errorf("structured filter: %+v", res.Items)
	}

	// Sorting by a field, both ways, with documents lacking a value last; paging works.
	// (The documents created for the validation checks have no values and sort last.)
	first3 := func(path string) []string { return titles(path)[:3] }
	eq("sort amount asc", first3("/api/v1/documents?sort=cf:"+amount.ID+"&q=custom+field+document"), "small", "medium", "large")
	eq("sort amount desc", first3("/api/v1/documents?sort=-cf:"+amount.ID+"&q=custom+field+document"), "large", "medium", "small")
	eq("sort date", first3("/api/v1/documents?sort=cf:"+due.ID+"&q=custom+field+document"), "small", "medium", "large")
	var page struct {
		Items []docDTO `json:"items"`
		Next  *string  `json:"next_cursor"`
	}
	c.do("GET", "/api/v1/documents?sort=cf:"+amount.ID+"&limit=2&q=custom+field+document", nil, 200, &page)
	if len(page.Items) != 2 || page.Next == nil {
		t.Fatalf("first page: %+v", page)
	}
	c.do("GET", "/api/v1/documents?sort=cf:"+amount.ID+"&limit=2&q=custom+field+document&cursor="+*page.Next, nil, 200, &page)
	if len(page.Items) != 2 || page.Items[0].Title != "large" || page.Next == nil {
		t.Errorf("second page: %+v", page.Items)
	}
	c.do("GET", "/api/v1/documents?sort=cf:00000000-0000-4000-8000-000000000000", nil, 422, nil)

	// Clearing a value, and the value is searchable as text too.
	c.do("PATCH", "/api/v1/documents/"+docs[0].ID, map[string]any{"custom_fields": map[string]any{status.ID: nil}}, 200, nil)
	eq("cleared", titles("/api/v1/documents?q=cf:Status&sort=title"), "medium")

	// Deleting a field removes its values.
	c.do("DELETE", "/api/v1/custom-fields/"+status.ID, nil, 204, nil)
	c.do("GET", "/api/v1/documents/"+docs[1].ID, nil, 200, &got)
	for _, f := range got.Fields {
		if f.Name == "Status" {
			t.Error("deleted field still has a value")
		}
	}

	// Moving a document to a space with a same-named field keeps the value; others are dropped.
	var space struct{ ID string }
	c.do("POST", "/api/v1/spaces", map[string]any{"name": "Work"}, 201, &space)
	c.do("POST", "/api/v1/custom-fields", map[string]any{"space_id": space.ID, "name": "amount", "data_type": "monetary"}, 201, nil)
	c.do("PATCH", "/api/v1/documents/"+docs[1].ID, map[string]any{"space_id": space.ID}, 200, &got)
	if len(got.Fields) != 1 || got.Fields[0].Name != "amount" || got.Fields[0].Value != 124500.5 {
		t.Errorf("values after move: %+v", got.Fields)
	}
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// totpNow computes the current authenticator code for a base32 secret (RFC 6238).
func totpNow(t *testing.T, secret string, offsetSteps int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(time.Now().Unix()/30+offsetSteps))
	m := hmac.New(sha1.New, key)
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) | (uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%06d", n%1_000_000)
}

// TestInvitations covers the invite link lifecycle.
func TestInvitations(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var created struct {
		Invite struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"invite"`
		Link string `json:"link"`
	}
	c.do("POST", "/api/v1/admin/invites", map[string]any{"email": "not-an-email"}, 422, nil)
	c.do("POST", "/api/v1/admin/invites", map[string]any{"spaces": []map[string]any{{"space_id": e.family, "role": "boss"}}}, 422, nil)
	c.do("POST", "/api/v1/admin/invites", map[string]any{"email": "admin@example.com"}, 409, nil) // already has an account
	c.do("POST", "/api/v1/admin/invites", map[string]any{"email": "Mia@Example.com", "display_name": "Mia", "send_email": true,
		"spaces": []map[string]any{{"space_id": e.family, "role": "editor"}}}, 201, &created)
	if created.Invite.Status != "pending" || !strings.Contains(created.Link, "/invite/dvt_inv_") {
		t.Fatalf("invite: %+v", created)
	}
	token := created.Link[strings.LastIndex(created.Link, "/")+1:]

	anon := newClient(t, e.srv.URL)
	var pv struct {
		Email string `json:"email"`
		By    string `json:"invited_by"`
	}
	anon.do("GET", "/api/v1/invites/"+token, nil, 200, &pv)
	if pv.Email != "mia@example.com" || pv.By != "Admin User" {
		t.Errorf("preview: %+v", pv)
	}
	anon.do("GET", "/api/v1/invites/dvt_inv_bogus", nil, 404, nil)
	anon.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"display_name": "Mia"}, 422, nil)                      // password needed
	anon.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"display_name": "Mia", "password": "short"}, 422, nil) // too short
	anon.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"display_name": "Mia", "email": "other@example.com", "password": "mia-long-password"}, 201, nil)
	anon.do("GET", "/api/v1/me", nil, 200, nil) // signed in by accepting

	// The invitation decided the address, and she's an editor in the invited space.
	var me struct {
		Email  string                            `json:"email"`
		Spaces []struct{ ID, Kind, Role string } `json:"spaces"`
	}
	anon.do("GET", "/api/v1/me", nil, 200, &me)
	if me.Email != "mia@example.com" || len(me.Spaces) != 2 {
		t.Fatalf("new member: %+v", me)
	}
	for _, s := range me.Spaces {
		if s.ID == e.family && s.Role != "editor" {
			t.Errorf("role in family space: %s", s.Role)
		}
	}
	// A link works once; used, revoked and unknown links look the same.
	anon.do("GET", "/api/v1/invites/"+token, nil, 404, nil)
	anon.do("POST", "/api/v1/invites/"+token+"/accept", map[string]any{"password": "mia-long-password"}, 404, nil)

	var second struct{ Link string }
	c.do("POST", "/api/v1/admin/invites", map[string]any{"display_name": "Raj"}, 201, &second)
	var list jsonList[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}]
	c.do("GET", "/api/v1/admin/invites", nil, 200, &list)
	if len(list.Items) != 2 {
		t.Fatalf("invites: %+v", list.Items)
	}
	for _, i := range list.Items {
		if i.Status == "pending" {
			c.do("DELETE", "/api/v1/admin/invites/"+i.ID, nil, 204, nil)
		}
	}
	anon2 := newClient(t, e.srv.URL)
	anon2.do("GET", "/api/v1/invites/"+second.Link[strings.LastIndex(second.Link, "/")+1:], nil, 404, nil)
	// Only admins can manage invitations.
	anon.do("POST", "/api/v1/admin/invites", map[string]any{}, 403, nil)
}

// TestTwoFactor covers enrolment, sign-in with a code, recovery codes and admin reset.
func TestTwoFactor(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var st struct {
		Enabled bool `json:"enabled"`
		Left    int  `json:"recovery_codes_left"`
	}
	c.do("GET", "/api/v1/me/2fa", nil, 200, &st)
	if st.Enabled {
		t.Fatal("2FA on by default")
	}
	var setup struct{ Secret, URI string }
	c.do("POST", "/api/v1/me/2fa/setup", nil, 200, &setup)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/") || setup.Secret == "" {
		t.Fatalf("setup: %+v", setup)
	}
	// Setup alone changes nothing about signing in.
	fresh := newClient(t, e.srv.URL)
	fresh.do("POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.com", "password": "a-long-password"}, 200, nil)

	c.do("POST", "/api/v1/me/2fa/enable", map[string]any{"code": "000000"}, 422, nil)
	var rec struct {
		Codes []string `json:"recovery_codes"`
	}
	c.do("POST", "/api/v1/me/2fa/enable", map[string]any{"code": totpNow(t, setup.Secret, 0)}, 200, &rec)
	if len(rec.Codes) != 10 {
		t.Fatalf("recovery codes: %v", rec.Codes)
	}
	c.do("GET", "/api/v1/me/2fa", nil, 200, &st)
	if !st.Enabled || st.Left != 10 {
		t.Fatalf("status: %+v", st)
	}

	// Sign-in now needs the code.
	var ch struct {
		Required  bool   `json:"two_factor_required"`
		Challenge string `json:"challenge"`
	}
	login := newClient(t, e.srv.URL)
	login.do("POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.com", "password": "a-long-password"}, 200, &ch)
	if !ch.Required || ch.Challenge == "" {
		t.Fatalf("expected a challenge: %+v", ch)
	}
	login.do("GET", "/api/v1/me", nil, 401, nil) // no session yet
	login.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": ch.Challenge, "code": "123456"}, 401, nil)
	login.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": "forged~1.abc", "code": "123456"}, 401, nil)
	// The step used to enable 2FA can't be replayed; the next step is accepted (clock drift allowance).
	login.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": ch.Challenge, "code": totpNow(t, setup.Secret, 0)}, 401, nil)
	login.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": ch.Challenge, "code": totpNow(t, setup.Secret, 1)}, 200, nil)
	login.do("GET", "/api/v1/me", nil, 200, nil)

	// Recovery codes work once.
	l2 := newClient(t, e.srv.URL)
	l2.do("POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.com", "password": "a-long-password"}, 200, &ch)
	l2.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": ch.Challenge, "code": rec.Codes[0]}, 200, nil)
	l3 := newClient(t, e.srv.URL)
	l3.do("POST", "/api/v1/auth/login", map[string]any{"email": "admin@example.com", "password": "a-long-password"}, 200, &ch)
	l3.do("POST", "/api/v1/auth/login/2fa", map[string]any{"challenge": ch.Challenge, "code": rec.Codes[0]}, 401, nil)
	c.do("GET", "/api/v1/me/2fa", nil, 200, &st)
	if st.Left != 9 {
		t.Errorf("recovery codes left: %d, want 9", st.Left)
	}

	// Turning it off needs the password and a code.
	c.do("POST", "/api/v1/me/2fa/disable", map[string]any{"password": "wrong", "code": rec.Codes[1]}, 422, nil)
	c.do("POST", "/api/v1/me/2fa/disable", map[string]any{"password": "a-long-password", "code": "000000"}, 422, nil)
	c.do("POST", "/api/v1/me/2fa/disable", map[string]any{"password": "a-long-password", "code": rec.Codes[1]}, 204, nil)
	c.do("GET", "/api/v1/me/2fa", nil, 200, &st)
	if st.Enabled {
		t.Fatal("still enabled")
	}

	// An administrator can reset another user's second factor.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	var ks struct{ Secret string }
	kid.do("POST", "/api/v1/me/2fa/setup", nil, 200, &ks)
	kid.do("POST", "/api/v1/me/2fa/enable", map[string]any{"code": totpNow(t, ks.Secret, 0)}, 200, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	for _, d := range dir.Items {
		if d.Email == "kid@example.com" {
			c.do("DELETE", "/api/v1/admin/users/"+d.ID+"/2fa", nil, 204, nil)
			c.do("DELETE", "/api/v1/admin/users/"+d.ID+"/2fa", nil, 404, nil)
		}
	}
	kid2 := newClient(t, e.srv.URL)
	kid2.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid2.do("GET", "/api/v1/me", nil, 200, nil)
}

// TestShareLinks covers public links: plain, password-protected, view-only, expiry and revocation.
func TestShareLinks(t *testing.T) {
	e := newEnv(t)
	c := e.c
	doc := c.upload(e.family, "shared.pdf", minimalPDF("a document that will be shared by link"))
	c.waitStatus(doc.ID, "ready")
	token := func(link string) string { return link[strings.LastIndex(link, "/")+1:] }

	type created struct {
		Share struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"share"`
		Link string `json:"link"`
	}
	c.do("POST", "/api/v1/shares", map[string]any{}, 422, nil)
	c.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID, "view_id": doc.ID}, 422, nil)
	c.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID, "password": "abc"}, 422, nil)
	c.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID, "expires_in_days": 0}, 422, nil)

	var plain created
	c.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID, "note": "for the accountant"}, 201, &plain)
	if !strings.Contains(plain.Link, "/s/dvt_shr_") {
		t.Fatalf("link: %s", plain.Link)
	}
	anon := newClient(t, e.srv.URL)
	var pub struct {
		Title    string `json:"title"`
		Password bool   `json:"requires_password"`
		Download bool   `json:"allow_download"`
		Docs     []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"documents"`
	}
	anon.do("GET", "/api/v1/public/shares/"+token(plain.Link), nil, 200, &pub)
	if pub.Password || pub.Download || len(pub.Docs) != 1 || pub.Docs[0].ID != doc.ID {
		t.Fatalf("public share: %+v", pub)
	}
	anon.raw("GET", "/api/v1/public/shares/"+token(plain.Link)+"/documents/"+doc.ID+"/file", nil, nil, 200)
	anon.raw("GET", "/api/v1/public/shares/"+token(plain.Link)+"/documents/"+doc.ID+"/file?kind=thumbnail", nil, nil, 200)
	anon.do("GET", "/api/v1/public/shares/"+token(plain.Link)+"/documents/"+doc.ID+"/file?download=1", nil, 403, nil) // view only
	anon.do("GET", "/api/v1/public/shares/"+token(plain.Link)+"/documents/00000000-0000-4000-8000-000000000000/file", nil, 404, nil)
	anon.do("GET", "/api/v1/public/shares/dvt_shr_nope", nil, 404, nil)
	// A share link gives no access to the rest of the API.
	anon.do("GET", "/api/v1/documents/"+doc.ID, nil, 401, nil)

	// Password-protected, with downloads allowed.
	var locked created
	c.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID, "password": "open-sesame", "allow_download": true, "expires_in_days": 7}, 201, &locked)
	lt := token(locked.Link)
	var meta struct {
		Password bool  `json:"requires_password"`
		Docs     []any `json:"documents"`
	}
	anon.do("GET", "/api/v1/public/shares/"+lt, nil, 200, &meta)
	if !meta.Password || len(meta.Docs) != 0 {
		t.Fatalf("locked share leaked documents: %+v", meta)
	}
	anon.do("GET", "/api/v1/public/shares/"+lt+"/documents/"+doc.ID+"/file", nil, 401, nil)
	anon.do("POST", "/api/v1/public/shares/"+lt+"/unlock", map[string]any{"password": "wrong"}, 422, nil)
	anon.do("POST", "/api/v1/public/shares/"+lt+"/unlock", map[string]any{"password": "open-sesame"}, 204, nil)
	anon.do("GET", "/api/v1/public/shares/"+lt, nil, 200, &meta)
	if meta.Password || len(meta.Docs) != 1 {
		t.Fatalf("after unlock: %+v", meta)
	}
	if b := anon.raw("GET", "/api/v1/public/shares/"+lt+"/documents/"+doc.ID+"/file?kind=original&download=1", nil, nil, 200); !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Error("download isn't the PDF")
	}
	// Another visitor doesn't inherit the unlock.
	other := newClient(t, e.srv.URL)
	other.do("GET", "/api/v1/public/shares/"+lt+"/documents/"+doc.ID+"/file", nil, 401, nil)
	// Too many wrong passwords are throttled.
	for i := 0; i < 12; i++ {
		other.raw("POST", "/api/v1/public/shares/"+lt+"/unlock", bytes.NewReader(mustJSON(map[string]any{"password": "nope"})),
			map[string]string{"Content-Type": "application/json"}, map[bool]int{true: 422, false: 429}[i < 8])
	}

	// Access is counted; the owner sees it.
	var list jsonList[struct {
		ID     string `json:"id"`
		Count  int    `json:"access_count"`
		Status string `json:"status"`
	}]
	c.do("GET", "/api/v1/shares?document_id="+doc.ID, nil, 200, &list)
	if len(list.Items) != 2 {
		t.Fatalf("shares: %+v", list.Items)
	}

	// Expiry and revocation.
	if _, err := e.a.Pool.Exec(e.ctx, `UPDATE shares SET expires_at=now()-interval '1 minute' WHERE id=$1`, locked.Share.ID); err != nil {
		t.Fatal(err)
	}
	anon.do("GET", "/api/v1/public/shares/"+lt, nil, 404, nil)
	c.do("DELETE", "/api/v1/shares/"+plain.Share.ID, nil, 204, nil)
	anon.do("GET", "/api/v1/public/shares/"+token(plain.Link), nil, 404, nil)
	c.do("DELETE", "/api/v1/shares/"+plain.Share.ID, nil, 404, nil)

	// A viewer can't share; a saved view can be shared once it belongs to a space.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	for _, d := range dir.Items {
		if d.Email == "kid@example.com" {
			c.do("PUT", "/api/v1/spaces/"+e.family+"/members/"+d.ID, map[string]any{"role": "viewer"}, 204, nil)
		}
	}
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid.do("POST", "/api/v1/shares", map[string]any{"document_id": doc.ID}, 403, nil)

	var personalView, spaceView struct{ ID string }
	c.do("POST", "/api/v1/saved-views", map[string]any{"name": "Mine", "query": map[string]any{"q": "shared"}}, 201, &personalView)
	c.do("POST", "/api/v1/shares", map[string]any{"view_id": personalView.ID}, 422, nil)
	c.do("POST", "/api/v1/saved-views", map[string]any{"name": "Shared docs", "space_id": e.family, "query": map[string]any{"q": "shared"}}, 201, &spaceView)
	var vs created
	c.do("POST", "/api/v1/shares", map[string]any{"view_id": spaceView.ID}, 201, &vs)
	anon.do("GET", "/api/v1/public/shares/"+token(vs.Link), nil, 200, &pub)
	if pub.Title != "Shared docs" || len(pub.Docs) != 1 || pub.Docs[0].ID != doc.ID {
		t.Fatalf("view share: %+v", pub)
	}
	anon.raw("GET", "/api/v1/public/shares/"+token(vs.Link)+"/documents/"+pub.Docs[0].ID+"/file", nil, nil, 200)
	// Documents outside the view stay out of reach.
	other2 := c.upload(e.family, "private.pdf", minimalPDF("this one is not part of the shared view at all"))
	anon.do("GET", "/api/v1/public/shares/"+token(vs.Link)+"/documents/"+other2.ID+"/file", nil, 404, nil)
}

// TestMCP talks to the MCP endpoint like an AI assistant would.
func TestMCP(t *testing.T) {
	e := newEnv(t)
	c := e.c
	doc := c.upload(e.family, "mcp.pdf", minimalPDF("Passport renewal receipt for the Kumar family, file BN1234567890"))
	c.waitStatus(doc.ID, "ready")
	private := c.upload(e.family, "other.pdf", minimalPDF("An unrelated warranty certificate for a laptop"))
	c.waitStatus(private.ID, "ready")

	var tok struct{ Secret string }
	c.do("POST", "/api/v1/me/tokens", map[string]any{"name": "assistant", "scopes": []string{"documents:read"}}, 201, &tok)
	var up struct{ Secret string }
	c.do("POST", "/api/v1/me/tokens", map[string]any{"name": "uploader", "scopes": []string{"upload"}}, 201, &up)
	assistant := newClient(t, e.srv.URL)
	assistant.token = tok.Secret

	rpc := func(cl *client, id int, method string, params any, want int) map[string]any {
		t.Helper()
		var out map[string]any
		body := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id > 0 {
			body["id"] = id
		}
		b := cl.raw("POST", "/mcp", bytes.NewReader(mustJSON(body)), map[string]string{"Content-Type": "application/json"}, want)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &out)
		}
		return out
	}
	call := func(name string, args map[string]any) (text string, isErr bool) {
		t.Helper()
		out := rpc(assistant, 1, "tools/call", map[string]any{"name": name, "arguments": args}, 200)
		res, _ := out["result"].(map[string]any)
		if res == nil {
			t.Fatalf("tool %s: %+v", name, out)
		}
		isErr, _ = res["isError"].(bool)
		content, _ := res["content"].([]any)
		if len(content) == 0 {
			t.Fatalf("tool %s returned no content: %+v", name, res)
		}
		return content[0].(map[string]any)["text"].(string), isErr
	}

	init := rpc(assistant, 1, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}}, 200)
	if info, _ := init["result"].(map[string]any)["serverInfo"].(map[string]any); info["name"] != "docveta" {
		t.Fatalf("initialize: %+v", init)
	}
	rpc(assistant, 0, "notifications/initialized", nil, 202)
	rpc(assistant, 2, "ping", nil, 200)
	list := rpc(assistant, 3, "tools/list", nil, 200)
	if tools, _ := list["result"].(map[string]any)["tools"].([]any); len(tools) < 6 {
		t.Fatalf("tools: %+v", list)
	}
	if out := rpc(assistant, 4, "nope/nothing", nil, 200); out["error"] == nil {
		t.Error("unknown method should be a JSON-RPC error")
	}

	text, bad := call("search_documents", map[string]any{"query": "passport renewal"})
	if bad || !strings.Contains(text, doc.ID) || strings.Contains(text, private.ID) {
		t.Errorf("search: %v %s", bad, text)
	}
	text, _ = call("get_document_text", map[string]any{"id": doc.ID})
	if !strings.Contains(text, "BN1234567890") || !strings.Contains(text, "page 1") {
		t.Errorf("text: %s", text)
	}
	text, _ = call("get_document", map[string]any{"id": doc.ID})
	if !strings.Contains(text, `"title": "mcp"`) {
		t.Errorf("document: %s", text)
	}
	if text, bad = call("get_document", map[string]any{"id": "nope"}); !bad {
		t.Errorf("a bad id should be a tool error: %s", text)
	}
	if text, bad = call("get_document", map[string]any{"id": "00000000-0000-4000-8000-000000000000"}); !bad || !strings.Contains(text, "not found") {
		t.Errorf("missing document: %v %s", bad, text)
	}
	if text, _ = call("list_spaces", nil); !strings.Contains(text, "Family") {
		t.Errorf("spaces: %s", text)
	}
	if text, _ = call("list_tags", map[string]any{"space": "Family"}); !strings.HasPrefix(strings.TrimSpace(text), "[") {
		t.Errorf("tags: %s", text)
	}
	if text, bad = call("list_tags", map[string]any{"space": "Nowhere"}); !bad {
		t.Errorf("unknown space: %s", text)
	}
	out := rpc(assistant, 5, "tools/call", map[string]any{"name": "shred_everything"}, 200)
	if out["error"] == nil {
		t.Error("unknown tool should be an error")
	}

	// Authentication: browser sessions and tokens without read access are refused.
	body := bytes.NewReader(mustJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}))
	c.raw("POST", "/mcp", body, map[string]string{"Content-Type": "application/json"}, 401)
	uploader := newClient(t, e.srv.URL)
	uploader.token = up.Secret
	uploader.raw("POST", "/mcp", bytes.NewReader(mustJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})), map[string]string{"Content-Type": "application/json"}, 401)
	newClient(t, e.srv.URL).raw("POST", "/mcp", bytes.NewReader(mustJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})), nil, 401)
	assistant.raw("POST", "/mcp", bytes.NewReader(mustJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})),
		map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, 403)
	assistant.raw("GET", "/mcp", nil, nil, 405)
}

// docx builds the smallest file the content sniffer recognises as a Word document.
func docx() []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": "<w:document/>"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return b.Bytes()
}

// TestOfficeDocuments converts a Word file through a (fake) Gotenberg server.
func TestOfficeDocuments(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var converted atomic.Int32
	gotenberg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/forms/libreoffice/convert" && r.Method == http.MethodPost:
			converted.Add(1)
			if err := r.ParseMultipartForm(10 << 20); err != nil || len(r.MultipartForm.File["files"]) != 1 {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			fn := r.MultipartForm.File["files"][0].Filename
			if !strings.HasSuffix(fn, ".docx") {
				http.Error(w, "unexpected file name "+fn, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(minimalPDF("This quarterly report was converted from a word processor document by Gotenberg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gotenberg.Close()

	// Without a converter Office files are refused politely.
	c.uploadStatus(e.family, "report.docx", docx(), 415)
	var info struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
	}
	c.do("GET", "/api/v1/admin/settings/office", nil, 200, &info)
	if info.Enabled {
		t.Fatalf("converter enabled by default: %+v", info)
	}
	c.do("PUT", "/api/v1/admin/settings/office", map[string]any{"url": "nonsense"}, 422, nil)
	c.do("POST", "/api/v1/admin/settings/office/test", map[string]any{"url": "http://127.0.0.1:1"}, 422, nil)
	c.do("POST", "/api/v1/admin/settings/office/test", map[string]any{"url": gotenberg.URL}, 204, nil)
	c.do("PUT", "/api/v1/admin/settings/office", map[string]any{"url": gotenberg.URL + "/"}, 200, &info)
	if !info.Enabled || info.URL != gotenberg.URL {
		t.Fatalf("settings not saved: %+v", info)
	}

	var d docDTO
	if err := json.Unmarshal(c.uploadStatus(e.family, "Quarterly report.docx", docx(), 201), &d); err != nil {
		t.Fatal(err)
	}
	d = c.waitStatus(d.ID, "ready")
	if !d.HasDerived || d.PageCount == nil || *d.PageCount != 1 || !d.HasThumbnail {
		t.Fatalf("converted document: %+v", d)
	}
	if got := e.list("/api/v1/documents?q=gotenberg"); len(got) != 1 || got[0].ID != d.ID {
		t.Fatalf("converted text not searchable: %+v", got)
	}
	pdf := c.raw("GET", "/api/v1/documents/"+d.ID+"/file?kind=derived", nil, nil, 200)
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Error("the converted copy isn't a PDF")
	}
	orig := c.raw("GET", "/api/v1/documents/"+d.ID+"/file?kind=original", nil, nil, 200)
	if !bytes.HasPrefix(orig, []byte("PK")) {
		t.Error("the original must stay a Word file")
	}

	// A converter that is down fails that document with a clear message; the file stays downloadable.
	gotenberg.Close()
	var failed docDTO
	if err := json.Unmarshal(c.uploadStatus(e.family, "second.docx", append(docx(), 0), 201), &failed); err != nil {
		t.Fatal(err)
	}
	c.wait(failed.ID, func(x docDTO) bool { return x.Status == "failed" })
	c.raw("GET", "/api/v1/documents/"+failed.ID+"/file?kind=original", nil, nil, 200)
}

// TestWatchedFolder drops files into a folder and checks they are imported, filed and cleaned up.
func TestWatchedFolder(t *testing.T) {
	e := newEnv(t)
	c := e.c
	root := filepath.Join(e.a.Cfg.DataDir, "watch")
	inbox := filepath.Join(root, "scans")

	var list struct {
		Roots []string `json:"roots"`
	}
	c.do("GET", "/api/v1/admin/folders", nil, 200, &list)
	if len(list.Roots) != 1 {
		t.Fatalf("roots: %+v", list.Roots)
	}
	c.do("POST", "/api/v1/admin/folders", map[string]any{"path": t.TempDir(), "space_id": e.family}, 422, nil) // outside the allowed root
	c.do("POST", "/api/v1/admin/folders", map[string]any{"path": filepath.Join(root, "..", "elsewhere"), "space_id": e.family}, 422, nil)
	c.do("POST", "/api/v1/admin/folders", map[string]any{"path": "scans", "space_id": e.family, "subfolders": "zigzag"}, 422, nil)
	var f struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	c.do("POST", "/api/v1/admin/folders", map[string]any{"path": "scans", "space_id": e.family, "subfolders": "tag", "stable_seconds": 0}, 201, &f)
	c.do("POST", "/api/v1/admin/folders", map[string]any{"path": "scans", "space_id": e.family}, 409, nil)

	write := func(rel string, data []byte) {
		t.Helper()
		p := filepath.Join(inbox, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write("water bill.pdf", minimalPDF("Water bill for the month of September, amount due 640 rupees"))
	write("Taxes/form 16.pdf", minimalPDF("Form 16 salary certificate for the assessment year 2026-27"))
	write("half copied.pdf.part", []byte("%PDF-1.4 incomplete"))
	write("junk.bin", []byte{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe})

	var res struct{ Imported, Failed, Skipped int }
	c.do("POST", "/api/v1/admin/folders/"+f.ID+"/scan", nil, 200, &res)
	if res.Imported != 2 || res.Failed != 1 {
		t.Fatalf("scan: %+v", res)
	}
	var docs jsonList[struct {
		ID     string                  `json:"id"`
		Title  string                  `json:"title"`
		Source string                  `json:"source"`
		Tags   []struct{ Name string } `json:"tags"`
	}]
	c.do("GET", "/api/v1/documents?sort=title", nil, 200, &docs)
	if len(docs.Items) != 2 {
		t.Fatalf("documents: %+v", docs.Items)
	}
	for _, d := range docs.Items {
		if d.Source != "folder" {
			t.Errorf("%s: source %q", d.Title, d.Source)
		}
		if d.Title == "form 16" && (len(d.Tags) != 1 || d.Tags[0].Name != "Taxes") {
			t.Errorf("sub-folder should become a tag: %+v", d.Tags)
		}
	}
	// Imported files left the inbox; the unreadable one is in failed/ with the reason.
	if _, err := os.Stat(filepath.Join(inbox, "water bill.pdf")); err == nil {
		t.Error("imported file still in the folder")
	}
	done, _ := filepath.Glob(filepath.Join(inbox, "done", "*"))
	failed, _ := filepath.Glob(filepath.Join(inbox, "failed", "*"))
	if len(done) != 2 || len(failed) != 2 { // the file and its .error.txt
		t.Errorf("done=%v failed=%v", done, failed)
	}
	for _, p := range failed {
		if strings.HasSuffix(p, ".error.txt") {
			if b, _ := os.ReadFile(p); !strings.Contains(string(b), "isn't supported") {
				t.Errorf("error note: %q", b)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(inbox, "half copied.pdf.part")); err != nil {
		t.Error("a partial copy must be left alone")
	}

	// The same file again is recognised as already imported (no duplicate, no failure).
	write("again.pdf", minimalPDF("Water bill for the month of September, amount due 640 rupees"))
	c.do("POST", "/api/v1/admin/folders/"+f.ID+"/scan", nil, 200, &res)
	if res.Imported != 1 || res.Failed != 0 {
		t.Errorf("duplicate scan: %+v", res)
	}
	c.do("GET", "/api/v1/documents", nil, 200, &docs)
	if len(docs.Items) != 2 {
		t.Errorf("a duplicate was imported: %d documents", len(docs.Items))
	}

	// Files still being copied wait: with a 60 s stability window a fresh file is skipped.
	c.do("PATCH", "/api/v1/admin/folders/"+f.ID, map[string]any{"stable_seconds": 60, "after_import": "delete"}, 200, nil)
	write("fresh.pdf", minimalPDF("A brand new scan that is still being copied by the scanner"))
	c.do("POST", "/api/v1/admin/folders/"+f.ID+"/scan", nil, 200, &res)
	if res.Imported != 0 || res.Skipped != 1 {
		t.Errorf("fresh file: %+v", res)
	}
	old := time.Now().Add(-5 * time.Minute)
	_ = os.Chtimes(filepath.Join(inbox, "fresh.pdf"), old, old)
	c.do("POST", "/api/v1/admin/folders/"+f.ID+"/scan", nil, 200, &res) // first sighting of the settled file
	c.do("POST", "/api/v1/admin/folders/"+f.ID+"/scan", nil, 200, &res) // unchanged since: import
	if res.Imported != 1 {
		t.Errorf("settled file: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(inbox, "fresh.pdf")); err == nil {
		t.Error("\"delete after import\" left the file behind")
	}

	// Only administrators manage folders; deleting one stops the watching (files stay on disk).
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid.do("GET", "/api/v1/admin/folders", nil, 403, nil)
	c.do("DELETE", "/api/v1/admin/folders/"+f.ID, nil, 204, nil)
	c.do("DELETE", "/api/v1/admin/folders/"+f.ID, nil, 404, nil)
}

// TestWorkflows covers triggers, conditions, actions, ownership rules and loop safety.
func TestWorkflows(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var work struct{ ID string }
	c.do("POST", "/api/v1/spaces", map[string]any{"name": "Archive"}, 201, &work)
	var amount struct{ ID string }
	c.do("POST", "/api/v1/custom-fields", map[string]any{"space_id": e.family, "name": "Amount", "data_type": "monetary"}, 201, &amount)

	var wf struct {
		ID string `json:"id"`
	}
	// Validation.
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "", "trigger": "added", "actions": []any{}}, 422, nil)
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "x", "trigger": "whenever", "actions": []map[string]any{{"type": "run_ai"}}}, 422, nil)
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "x", "trigger": "added", "actions": []map[string]any{{"type": "teleport"}}}, 422, nil)
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "x", "trigger": "added", "actions": []map[string]any{{"type": "move_to_space", "space_id": e.family}}}, 422, nil)
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "x", "trigger": "added", "conditions": map[string]any{"bogus": 1}, "actions": []map[string]any{{"type": "run_ai"}}}, 422, nil)

	// "When a document from HDFC is added: tag it Bank, set the type, fill Amount, tell the owner."
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "HDFC statements", "trigger": "processed",
		"conditions": map[string]any{"q": "hdfc statement"},
		"actions": []map[string]any{
			{"type": "add_tags", "names": []string{"Bank", "Statements"}},
			{"type": "set_document_type", "name": "Bank statement"},
			{"type": "set_correspondent", "name": "HDFC Bank"},
			{"type": "set_field", "field_id": amount.ID, "value": 1250},
			{"type": "set_inbox", "value": false},
			{"type": "notify", "to": "owner", "title": "{{title}} was filed", "message": "From {{correspondent}}"},
		}}, 201, &wf)

	d := c.upload(e.family, "hdfc.pdf", minimalPDF("HDFC statement for the period ending September, balance forward"))
	c.waitStatus(d.ID, "ready")
	var full struct {
		Tags          []struct{ Name string } `json:"tags"`
		Type          *struct{ Name string }  `json:"document_type"`
		Correspondent *struct{ Name string }  `json:"correspondent"`
		Inbox         bool                    `json:"inbox"`
		Fields        []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"custom_fields"`
	}
	e.waitFor("workflow to apply", func() bool {
		c.do("GET", "/api/v1/documents/"+d.ID, nil, 200, &full)
		return full.Correspondent != nil
	})
	if len(full.Tags) != 2 || full.Type == nil || full.Type.Name != "Bank statement" || full.Inbox || len(full.Fields) != 1 || full.Fields[0].Value != 1250.0 {
		t.Fatalf("workflow result: %+v", full)
	}
	var notified int
	e.waitFor("notification", func() bool {
		_ = e.a.Pool.QueryRow(e.ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND event_type='workflow.notice' AND title='hdfc was filed'`, e.me).Scan(&notified)
		return notified == 1
	})
	var hist jsonList[struct {
		Action string `json:"action"`
	}]
	c.do("GET", "/api/v1/documents/"+d.ID+"/history", nil, 200, &hist)
	ran := false
	for _, h := range hist.Items {
		ran = ran || h.Action == "workflow_ran"
	}
	if !ran {
		t.Error("the workflow left no trace in the document history")
	}

	// A document that doesn't match is left alone.
	other := c.upload(e.family, "electric.pdf", minimalPDF("Electricity bill from the power company for September"))
	c.waitStatus(other.ID, "ready")
	time.Sleep(1500 * time.Millisecond)
	c.do("GET", "/api/v1/documents/"+other.ID, nil, 200, &full)
	if len(full.Tags) != 0 || full.Correspondent != nil || !full.Inbox {
		t.Errorf("a non-matching document was changed: %+v", full)
	}

	// Test (dry run) tells what would happen.
	var tr struct {
		Matches bool     `json:"matches"`
		Steps   []string `json:"steps"`
	}
	c.do("POST", "/api/v1/workflows/"+wf.ID+"/test", map[string]any{"document_id": d.ID}, 200, &tr)
	if !tr.Matches || len(tr.Steps) != 6 {
		t.Errorf("dry run: %+v", tr)
	}
	c.do("POST", "/api/v1/workflows/"+wf.ID+"/test", map[string]any{"document_id": other.ID}, 200, &tr)
	if tr.Matches {
		t.Errorf("dry run matched the wrong document: %+v", tr)
	}

	// A person's own choice is never overridden: they change the type, then reprocess.
	c.do("PATCH", "/api/v1/documents/"+d.ID, map[string]any{"correspondent_id": nil}, 200, nil)
	var corr struct{ ID string }
	c.do("POST", "/api/v1/correspondents", map[string]any{"space_id": e.family, "name": "Somebody else"}, 201, &corr)
	c.do("PATCH", "/api/v1/documents/"+d.ID, map[string]any{"correspondent_id": corr.ID}, 200, nil)
	c.do("POST", "/api/v1/documents/"+d.ID+"/reprocess", nil, 204, nil)
	c.waitStatus(d.ID, "ready")
	time.Sleep(2 * time.Second)
	c.do("GET", "/api/v1/documents/"+d.ID, nil, 200, &full)
	if full.Correspondent == nil || full.Correspondent.Name != "Somebody else" {
		t.Errorf("a workflow overrode a human's choice: %+v", full.Correspondent)
	}

	// "updated" workflows fire on edits, and their own changes don't loop.
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "Tax means review", "trigger": "updated",
		"conditions": map[string]any{"q": "tag:tax"},
		"actions":    []map[string]any{{"type": "add_tags", "names": []string{"Seen by workflow"}}, {"type": "set_inbox", "value": true}}}, 201, &wf)
	var tax struct{ ID string }
	c.do("POST", "/api/v1/tags", map[string]any{"space_id": e.family, "name": "Tax"}, 201, &tax)
	c.do("PATCH", "/api/v1/documents/"+other.ID, map[string]any{"add_tag_ids": []string{tax.ID}, "inbox": false}, 200, nil)
	e.waitFor("update workflow", func() bool {
		c.do("GET", "/api/v1/documents/"+other.ID, nil, 200, &full)
		return len(full.Tags) == 2
	})
	time.Sleep(2 * time.Second) // a loop would keep adding runs
	var runs jsonList[struct{ Status string }]
	c.do("GET", "/api/v1/workflows/"+wf.ID+"/runs", nil, 200, &runs)
	if len(runs.Items) != 1 || !full.Inbox {
		t.Errorf("update workflow: %d runs, inbox=%v", len(runs.Items), full.Inbox)
	}

	// Moving to another space (as a workflow action) maps tags by name and stops further rules.
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "Archive receipts", "trigger": "added",
		"conditions": map[string]any{"q": "receipt"}, "actions": []map[string]any{{"type": "move_to_space", "space_id": work.ID}}}, 201, nil)
	rc := c.upload(e.family, "receipt.pdf", minimalPDF("Receipt for a purchase of office furniture from the shop"))
	c.waitStatus(rc.ID, "ready")
	e.waitFor("move", func() bool {
		var d docDTO
		c.do("GET", "/api/v1/documents/"+rc.ID, nil, 200, &d)
		return d.Space.ID == work.ID
	})

	// Scheduled workflows process matching documents once.
	var sched struct{ ID string }
	c.do("POST", "/api/v1/workflows", map[string]any{"space_id": e.family, "name": "Nightly", "trigger": "schedule", "schedule_time": "00:00",
		"conditions": map[string]any{"q": "electricity"}, "actions": []map[string]any{{"type": "add_tags", "names": []string{"Nightly"}}}}, 201, &sched)
	if err := e.a.Workflows.RunScheduled(e.ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	c.do("GET", "/api/v1/documents/"+other.ID, nil, 200, &full)
	nightly := false
	for _, tg := range full.Tags {
		nightly = nightly || tg.Name == "Nightly"
	}
	if !nightly {
		t.Errorf("scheduled workflow didn't run: %+v", full.Tags)
	}
	// Only owners manage workflows.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	var dir struct{ Items []struct{ ID, Email string } }
	c.do("GET", "/api/v1/users/directory", nil, 200, &dir)
	for _, u := range dir.Items {
		if u.Email == "kid@example.com" {
			c.do("PUT", "/api/v1/spaces/"+e.family+"/members/"+u.ID, map[string]any{"role": "editor"}, 204, nil)
		}
	}
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid.do("GET", "/api/v1/workflows?space_id="+e.family, nil, 200, nil)
	kid.do("DELETE", "/api/v1/workflows/"+sched.ID, nil, 403, nil)
	c.do("DELETE", "/api/v1/workflows/"+sched.ID, nil, 204, nil)
}

// tusReq makes a tus request with the protocol header.
func tusReq(t *testing.T, c *client, method, path string, body []byte, hdr map[string]string, want int) http.Header {
	t.Helper()
	req, _ := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	req.Header.Set("Tus-Resumable", "1.0.0")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, want, b)
	}
	return res.Header
}

func meta(kv map[string]string) string {
	var parts []string
	for k, v := range kv {
		parts = append(parts, k+" "+base64.StdEncoding.EncodeToString([]byte(v)))
	}
	return strings.Join(parts, ",")
}

// TestResumableUploads sends a document in pieces, "drops" the connection part-way and resumes.
func TestResumableUploads(t *testing.T) {
	e := newEnv(t)
	c := e.c
	pdf := minimalPDF("A long scan that was uploaded in several pieces over a flaky network connection")
	md := meta(map[string]string{"space_id": e.family, "filename": "big scan.pdf", "title": "Resumed upload"})

	// Capabilities and version negotiation.
	h := tusReq(t, c, "OPTIONS", "/api/v1/uploads", nil, nil, 204)
	if h.Get("Tus-Version") != "1.0.0" || !strings.Contains(h.Get("Tus-Extension"), "creation") || h.Get("Tus-Max-Size") == "" {
		t.Errorf("OPTIONS headers: %v", h)
	}
	req, _ := http.NewRequest("POST", c.base+"/api/v1/uploads", nil)
	req.Header.Set("Tus-Resumable", "0.2.0")
	if res, err := c.h.Do(req); err != nil || res.StatusCode != 412 {
		t.Errorf("wrong tus version should be refused with 412: %v %v", res, err)
	}
	tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "abc"}, 422)
	tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "10", "Upload-Metadata": meta(map[string]string{"space_id": "nope"})}, 422)
	tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "99999999999"}, 413)
	anon := newClient(t, e.srv.URL)
	tusReq(t, anon, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "10"}, 401)

	loc := tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": strconv.Itoa(len(pdf)), "Upload-Metadata": md}, 201).Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/uploads/") {
		t.Fatalf("location: %q", loc)
	}
	if h := tusReq(t, c, "HEAD", loc, nil, nil, 200); h.Get("Upload-Offset") != "0" || h.Get("Upload-Length") != strconv.Itoa(len(pdf)) {
		t.Errorf("HEAD of a new upload: %v", h)
	}

	// First chunk, then a "dropped connection": the second request promises more than it sends.
	third := len(pdf) / 3
	chunk := map[string]string{"Content-Type": "application/offset+octet-stream"}
	chunk["Upload-Offset"] = "0"
	tusReq(t, c, "PATCH", loc, pdf[:third], chunk, 204)
	chunk["Upload-Offset"] = strconv.Itoa(third)
	broken, _ := http.NewRequest("PATCH", c.base+loc, io.MultiReader(bytes.NewReader(pdf[third:third+50]), errReader{}))
	broken.Header.Set("Tus-Resumable", "1.0.0")
	broken.Header.Set("Content-Type", "application/offset+octet-stream")
	broken.Header.Set("Upload-Offset", strconv.Itoa(third))
	_, _ = c.h.Do(broken) // fails mid-body, as a network drop would

	// Resume: ask where we are, then continue from there.
	var offset int
	e.waitFor("server to note partial progress", func() bool {
		h := tusReq(t, c, "HEAD", loc, nil, nil, 200)
		offset, _ = strconv.Atoi(h.Get("Upload-Offset"))
		return offset > third
	})
	if offset > third+50 {
		t.Fatalf("server claims %d bytes, only %d were sent", offset, third+50)
	}
	// Wrong offsets are refused with the real one.
	chunk["Upload-Offset"] = "1"
	if h := tusReq(t, c, "PATCH", loc, pdf[1:5], chunk, 409); h.Get("Upload-Offset") != strconv.Itoa(offset) {
		t.Errorf("409 should report the real offset %d, got %v", offset, h)
	}
	chunk["Upload-Offset"] = strconv.Itoa(offset)
	tusReq(t, c, "PATCH", loc, pdf[offset:offset+30], map[string]string{"Content-Type": "text/plain", "Upload-Offset": strconv.Itoa(offset)}, 415)
	h = tusReq(t, c, "PATCH", loc, pdf[offset:], chunk, 204)
	docID := h.Get("Docveta-Document-Id")
	if docID == "" {
		t.Fatalf("no document id after the last chunk: %v", h)
	}
	d := c.waitStatus(docID, "ready")
	if d.Title != "Resumed upload" {
		t.Errorf("title: %q", d.Title)
	}
	if got := c.raw("GET", "/api/v1/documents/"+docID+"/file?kind=original", nil, nil, 200); !bytes.Equal(got, pdf) {
		t.Error("the assembled file differs from what was sent")
	}
	tusReq(t, c, "HEAD", loc, nil, nil, 404) // finished uploads are gone

	// A duplicate fails the last PATCH with the usual 409 problem.
	loc2 := tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": strconv.Itoa(len(pdf)), "Upload-Metadata": md}, 201).Get("Location")
	tusReq(t, c, "PATCH", loc2, pdf, map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": "0"}, 409)
	// ...unless it was allowed.
	md2 := meta(map[string]string{"space_id": e.family, "filename": "again.pdf", "allow_duplicate": "true"})
	loc3 := tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": strconv.Itoa(len(pdf)), "Upload-Metadata": md2}, 201).Get("Location")
	tusReq(t, c, "PATCH", loc3, pdf, map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": "0"}, 204)

	// Cancelling frees the upload; other people can't touch it; expired ones are cleaned up.
	loc4 := tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "100"}, 201).Get("Location")
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	kid := newClient(t, e.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	tusReq(t, kid, "HEAD", loc4, nil, nil, 404)
	tusReq(t, kid, "DELETE", loc4, nil, nil, 404)
	if _, err := e.a.Pool.Exec(e.ctx, `UPDATE uploads SET expires_at=now()-interval '1 hour' WHERE true`); err != nil {
		t.Fatal(err)
	}
	e.a.Uploads.Cleanup(e.ctx)
	tusReq(t, c, "HEAD", loc4, nil, nil, 404)
	loc5 := tusReq(t, c, "POST", "/api/v1/uploads", nil, map[string]string{"Upload-Length": "100"}, 201).Get("Location")
	tusReq(t, c, "DELETE", loc5, nil, nil, 204)
	tusReq(t, c, "HEAD", loc5, nil, nil, 404)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("connection reset") }

type pdfPage struct {
	text  string
	image image.Image // drawn at (x, y) with size w x h points; nil for text-only pages
	x, y  int
	w, h  int
}

// scanPDF builds a PDF whose pages carry text and/or an image (a barcode sheet or label).
func scanPDF(pages []pdfPage) []byte {
	var b bytes.Buffer
	var off []int
	obj := func(body string) int {
		off = append(off, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(off), body)
		return len(off)
	}
	stream := func(dict string, data []byte) int {
		off = append(off, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n<< %s /Length %d >>\nstream\n", len(off), dict, len(data))
		b.Write(data)
		b.WriteString("\nendstream\nendobj\n")
		return len(off)
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	pagesObj := obj("PLACEHOLDER") // object 2, rewritten below
	font := obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	var kids []string
	for _, p := range pages {
		xobj, draw := "", ""
		if p.image != nil {
			g := image.NewGray(p.image.Bounds())
			for y := 0; y < g.Bounds().Dy(); y++ {
				for x := 0; x < g.Bounds().Dx(); x++ {
					g.Set(x, y, p.image.At(x, y))
				}
			}
			var z bytes.Buffer
			zw := zlib.NewWriter(&z)
			_, _ = zw.Write(g.Pix)
			_ = zw.Close()
			img := stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", g.Bounds().Dx(), g.Bounds().Dy()), z.Bytes())
			xobj = fmt.Sprintf("/XObject << /Im0 %d 0 R >>", img)
			draw = fmt.Sprintf("q %d 0 0 %d %d %d cm /Im0 Do Q ", p.w, p.h, p.x, p.y)
		}
		text := ""
		if p.text != "" {
			text = fmt.Sprintf("BT /F1 12 Tf 72 100 Td (%s) Tj ET", p.text)
		}
		content := stream("", []byte(draw+text))
		page := obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> %s >> /Contents %d 0 R >>", font, xobj, content))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	// Rewrite object 2 now that the page objects exist (same length keeps offsets valid only if padded).
	body := fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	all := b.Bytes()
	start := off[pagesObj-1]
	head := fmt.Sprintf("%d 0 obj\n", pagesObj)
	oldEnd := bytes.Index(all[start:], []byte("endobj\n")) + len("endobj\n")
	rebuilt := append([]byte{}, all[:start]...)
	rebuilt = append(rebuilt, []byte(head+body+"\nendobj\n")...)
	delta := len(head+body+"\nendobj\n") - oldEnd
	rebuilt = append(rebuilt, all[start+oldEnd:]...)
	for i := pagesObj; i < len(off); i++ {
		off[i] += delta
	}
	var out bytes.Buffer
	out.Write(rebuilt)
	x := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(off)+1)
	for _, o := range off {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(off)+1, x)
	return out.Bytes()
}

// TestBatchScanning splits a batch at separator sheets and reads archive-number labels.
func TestBatchScanning(t *testing.T) {
	e := newEnv(t)
	c := e.c
	// Printable sheets are served.
	if b := c.raw("GET", "/api/v1/barcodes/separator.png", nil, nil, 200); !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Error("separator sheet isn't a PNG")
	}
	if b := c.raw("GET", "/api/v1/barcodes/asn.png?n=42", nil, nil, 200); !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Error("label isn't a PNG")
	}
	c.do("GET", "/api/v1/barcodes/asn.png?n=zero", nil, 422, nil)

	sep, _ := barcode.SeparatorSheet()
	label7, _ := barcode.ASNLabel(7)
	label42, _ := barcode.ASNLabel(42)
	sepPage := pdfPage{image: sep, x: 0, y: 0, w: 595, h: 842}
	text := func(s string) pdfPage { return pdfPage{text: s} }
	batch := scanPDF([]pdfPage{
		text("First document page one with enough words to count as real text"),
		text("First document page two with enough words to count as real text"),
		sepPage,
		{text: "Second document starts here with a label on its first page", image: label7, x: 40, y: 700, w: label7.Bounds().Dx() / 2, h: label7.Bounds().Dy() / 2},
		sepPage,
		text("Third document is a single page with plenty of words as well"),
	})

	// Off by default: the batch stays one document.
	plain := c.upload(e.family, "batch-off.pdf", batch)
	plain = c.waitStage(plain.ID, "awaiting_ocr") // the separator pages are pictures, so they wait for text recognition
	if plain.PageCount == nil || *plain.PageCount != 6 {
		t.Fatalf("splitting happened while off: %+v", plain.PageCount)
	}

	c.do("PATCH", "/api/v1/spaces/"+e.family, map[string]any{"split_on_separators": true, "read_asn_barcodes": true}, 200, nil)
	// A slightly different file, so it isn't a duplicate of the first upload.
	b2 := scanPDF([]pdfPage{
		text("First document page one with enough words to count as real text"),
		text("First document page two with enough words to count as real text"),
		sepPage,
		{text: "Second document starts here with a label on its first page", image: label7, x: 40, y: 700, w: label7.Bounds().Dx() / 2, h: label7.Bounds().Dy() / 2},
		sepPage,
		text("Third document is a single page with plenty of words as well too"),
	})
	orig := c.upload(e.family, "scanned batch.pdf", b2)
	var docs jsonList[struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		PageCount *int   `json:"page_count"`
		ASN       *int64 `json:"asn"`
		Source    string `json:"source"`
	}]
	e.waitFor("the batch to be split", func() bool {
		c.do("GET", "/api/v1/documents?q=%22scanned+batch%22&sort=title", nil, 200, &docs)
		return len(docs.Items) == 3
	})
	pages := []int{2, 1, 1}
	for i, d := range docs.Items {
		if d.Title != fmt.Sprintf("scanned batch (%d of 3)", i+1) || d.Source != "scan" {
			t.Errorf("part %d: %+v", i+1, d)
		}
		c.waitStatus(d.ID, "ready")
		var full docDTO
		c.do("GET", "/api/v1/documents/"+d.ID, nil, 200, &full)
		if full.PageCount == nil || *full.PageCount != pages[i] {
			t.Errorf("part %d has %v pages, want %d", i+1, full.PageCount, pages[i])
		}
	}
	var second struct {
		ASN *int64 `json:"asn"`
	}
	c.do("GET", "/api/v1/documents/"+docs.Items[1].ID, nil, 200, &second)
	if second.ASN == nil || *second.ASN != 7 {
		t.Errorf("the label on the second part should give it archive number 7, got %v", second.ASN)
	}
	// The original batch is in the Trash and can be restored.
	var gone docDTO
	c.do("GET", "/api/v1/documents/"+orig.ID, nil, 200, &gone)
	var trashed jsonList[docDTO]
	c.do("GET", "/api/v1/documents?trash=true", nil, 200, &trashed)
	found := false
	for _, d := range trashed.Items {
		found = found || d.ID == orig.ID
	}
	if !found {
		t.Error("the original batch should be in the Trash")
	}

	// A single document with a label (no separators) just gets its archive number.
	one := scanPDF([]pdfPage{{text: "A lone document with an archive label stuck on the first page", image: label42, x: 40, y: 700, w: label42.Bounds().Dx() / 2, h: label42.Bounds().Dy() / 2}})
	d1 := c.upload(e.family, "labelled.pdf", one)
	c.waitStatus(d1.ID, "ready")
	var lab struct {
		ASN *int64 `json:"asn"`
	}
	c.do("GET", "/api/v1/documents/"+d1.ID, nil, 200, &lab)
	if lab.ASN == nil || *lab.ASN != 42 {
		t.Errorf("archive number from label: %v", lab.ASN)
	}
}

// fakeS3 is a minimal in-memory S3 (path-style): enough for Docveta's blob store.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case bucket != "docs":
		http.Error(w, `<Error><Code>NoSuchBucket</Code></Error>`, http.StatusNotFound)
	case key == "" && r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		prefix := r.URL.Query().Get("prefix")
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>docs</Name><IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			fmt.Fprintf(w, `<Contents><Key>%s</Key><LastModified>2020-01-01T00:00:00.000Z</LastModified><ETag>"x"</ETag><Size>%d</Size></Contents>`, k, len(f.objects[k]))
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			b = decodeAWSChunked(b) // real S3 strips the per-chunk signatures; so must this fake
		}
		f.objects[key] = b
		f.puts++
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		b, ok := f.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				fmt.Fprint(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
			}
			return
		}
		w.Header().Set("ETag", `"etag"`)
		http.ServeContent(w, r, "", time.Unix(1700000000, 0), bytes.NewReader(b))
	default:
		http.Error(w, "unsupported", http.StatusNotImplemented)
	}
}

// TestS3Storage runs the document lifecycle with blobs in an (in-memory) S3 bucket.
func TestS3Storage(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}}
	s3 := httptest.NewServer(fake)
	defer s3.Close()
	e := newEnvWith(t, func(cfg *config.Config) {
		cfg.Storage = "s3"
		cfg.S3 = config.S3{Endpoint: s3.URL, Bucket: "docs", Region: "us-east-1", AccessKey: "key", SecretKey: "secret", PathStyle: true, Prefix: "docveta"}
	})
	c := e.c
	pdf := minimalPDF("A document whose bytes live in an S3 bucket instead of on the disk")
	d := c.upload(e.family, "s3.pdf", pdf)
	d = c.waitStatus(d.ID, "ready")
	if !d.HasThumbnail {
		t.Fatalf("thumbnail missing: %+v", d)
	}
	if got := c.raw("GET", "/api/v1/documents/"+d.ID+"/file?kind=original", nil, nil, 200); !bytes.Equal(got, pdf) {
		t.Error("download from S3 differs from the upload")
	}
	// Range requests (the PDF viewer uses them) work through the bucket.
	req, _ := http.NewRequest("GET", c.base+"/api/v1/documents/"+d.ID+"/file?kind=original", nil)
	req.Header.Set("Range", "bytes=0-4")
	res, err := c.h.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(part) != "%PDF-" {
		t.Errorf("range request: %d %q", res.StatusCode, part)
	}
	fake.mu.Lock()
	n := len(fake.objects)
	var keys []string
	for k := range fake.objects {
		keys = append(keys, k)
	}
	fake.mu.Unlock()
	if n < 2 || !strings.HasPrefix(keys[0], "docveta/sha256/") {
		t.Fatalf("objects in the bucket: %v", keys)
	}
	// Nothing is left behind on the local disk except scratch space.
	if left, _ := filepath.Glob(filepath.Join(e.a.Cfg.DataDir, "blobs", "*", "*")); len(left) != 0 {
		t.Errorf("blobs were written to the disk too: %v", left)
	}
	// Walk lists what is stored, and a missing object is a clean 404, not an error page.
	count := 0
	if err := e.a.Store.Walk(e.ctx, func(key string, _ time.Time) error { count++; return nil }); err != nil || count != n {
		t.Errorf("walk: %d objects (%v), want %d", count, err, n)
	}
	// The same file twice isn't uploaded twice.
	fake.mu.Lock()
	before := fake.puts
	fake.mu.Unlock()
	dup := c.uploadStatus(e.family, "again.pdf", append([]byte{}, pdf...), 409)
	_ = dup
	fake.mu.Lock()
	after := fake.puts
	fake.mu.Unlock()
	if after != before {
		t.Errorf("duplicate upload wrote %d more objects", after-before)
	}
	fake.mu.Lock()
	for k := range fake.objects {
		if strings.Contains(k, "sha256/") {
			delete(fake.objects, k)
		}
	}
	fake.mu.Unlock()
	c.do("GET", "/api/v1/documents/"+d.ID+"/file?kind=original", nil, 404, nil)
}

// decodeAWSChunked decodes "<hex size>;chunk-signature=...\r\n<data>\r\n" frames.
func decodeAWSChunked(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		i := bytes.Index(b, []byte("\r\n"))
		if i < 0 {
			break
		}
		head, _, _ := strings.Cut(string(b[:i]), ";")
		n, err := strconv.ParseInt(head, 16, 64)
		if err != nil || n == 0 {
			break
		}
		b = b[i+2:]
		out = append(out, b[:n]...)
		b = b[n+2:]
	}
	return out
}

// TestExportImport moves a whole installation into a fresh one and back again.
func TestExportImport(t *testing.T) {
	src := newEnv(t)
	c := src.c
	var tag, field, corr struct{ ID string }
	c.do("POST", "/api/v1/tags", map[string]any{"space_id": src.family, "name": "Tax", "color": "amber", "match_algorithm": "any", "match_pattern": "income"}, 201, &tag)
	c.do("POST", "/api/v1/correspondents", map[string]any{"space_id": src.family, "name": "Income Tax Dept"}, 201, &corr)
	c.do("POST", "/api/v1/custom-fields", map[string]any{"space_id": src.family, "name": "Amount", "data_type": "monetary"}, 201, &field)
	pdfA := minimalPDF("Income tax return acknowledgement for the assessment year 2026-27 with refund")
	a := c.upload(src.family, "return.pdf", pdfA)
	c.waitStatus(a.ID, "ready")
	c.do("PATCH", "/api/v1/documents/"+a.ID, map[string]any{"title": "Tax return 2026", "document_date": "2026-07-31", "correspondent_id": corr.ID,
		"custom_fields": map[string]any{field.ID: 18250.5}, "physical_location": "Blue folder"}, 200, nil)
	c.do("POST", "/api/v1/documents/"+a.ID+"/notes", map[string]any{"body": "Refund expected in September"}, 201, nil)
	b := c.upload(src.family, "other.pdf", minimalPDF("Gas connection agreement between the household and the supplier of cooking gas"))
	c.waitStatus(b.ID, "ready")
	gone := c.upload(src.family, "trashed.pdf", minimalPDF("This document is in the trash and must not be exported at all"))
	c.waitStatus(gone.ID, "ready")
	c.do("DELETE", "/api/v1/documents/"+gone.ID, nil, 204, nil)
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	c.do("POST", "/api/v1/saved-views", map[string]any{"name": "Taxes", "space_id": src.family, "query": map[string]any{"q": "tag:tax"}}, 201, nil)

	// Export to a folder with readable names, and as a zip through the admin API.
	dir := filepath.Join(t.TempDir(), "export")
	counts, err := exchange.Export(src.ctx, src.a.Pool, src.a.Store, exchange.DirSink(dir), exchange.ExportOptions{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if counts["documents"] != 2 || counts["notes"] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	readable := false
	for _, f := range files {
		readable = readable || (strings.HasPrefix(f, "files/Family/") && strings.Contains(f, "Tax return 2026 - ") && strings.HasSuffix(f, ".pdf"))
	}
	if !readable {
		t.Errorf("expected readable file names, got %v", files)
	}
	zipBytes := c.raw("GET", "/api/v1/admin/export", nil, nil, 200)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["manifest.json"] || !names["documents.jsonl"] {
		t.Errorf("zip entries: %v", names)
	}
	kid := newClient(t, src.srv.URL)
	kid.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	kid.do("GET", "/api/v1/admin/export", nil, 403, nil)

	// Import into a fresh installation.
	dst := newEnv(t)
	rep, err := exchange.Import(dst.ctx, dst.a.Pool, dst.a.Store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Documents != 2 || rep.Notes != 1 || rep.Users != 1 || len(rep.UnsetUsers) != 1 || len(rep.Warnings) != 0 {
		t.Fatalf("report: %+v", rep)
	}
	d := dst.c
	var list jsonList[struct {
		ID    string                  `json:"id"`
		Title string                  `json:"title"`
		Notes int                     `json:"note_count"`
		Date  string                  `json:"document_date"`
		Loc   string                  `json:"physical_location"`
		Corr  *struct{ Name string }  `json:"correspondent"`
		Tags  []struct{ Name string } `json:"tags"`
		Cust  []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"custom_fields"`
	}]
	d.do("GET", "/api/v1/documents?sort=title", nil, 200, &list)
	if len(list.Items) != 2 {
		t.Fatalf("imported documents: %+v", list.Items)
	}
	var tax = list.Items[1]
	if tax.Title != "Tax return 2026" || tax.Notes != 1 || tax.Date != "2026-07-31" || tax.Loc != "Blue folder" || tax.Corr == nil || tax.Corr.Name != "Income Tax Dept" ||
		len(tax.Cust) != 1 || tax.Cust[0].Value != 18250.5 {
		t.Errorf("metadata not preserved: %+v", tax)
	}
	// The text came along: it is searchable without any processing, and files download intact.
	var found jsonList[docDTO]
	d.do("GET", "/api/v1/documents?q=refund", nil, 200, &found)
	if len(found.Items) != 1 || found.Items[0].ID != tax.ID {
		t.Errorf("imported text isn't searchable: %+v", found.Items)
	}
	if got := d.raw("GET", "/api/v1/documents/"+tax.ID+"/file?kind=original", nil, nil, 200); !bytes.Equal(got, pdfA) {
		t.Error("the imported file differs from the original")
	}
	d.do("GET", "/api/v1/documents/"+tax.ID+"/thumbnail", nil, 200, nil)
	if got := d.raw("GET", "/api/v1/documents?trash=true", nil, nil, 200); strings.Contains(string(got), "trashed") {
		t.Error("a trashed document was exported")
	}
	var views jsonList[struct{ Name string }]
	d.do("GET", "/api/v1/saved-views", nil, 200, &views)
	if len(views.Items) != 1 || views.Items[0].Name != "Taxes" {
		t.Errorf("saved views: %+v", views.Items)
	}

	// Importing again changes nothing.
	rep, err = exchange.Import(dst.ctx, dst.a.Pool, dst.a.Store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Documents != 0 || rep.Skipped != 2 || rep.Users != 0 || rep.Spaces != 0 || len(rep.Warnings) != 0 {
		t.Errorf("second import: %+v", rep)
	}
	d.do("GET", "/api/v1/documents", nil, 200, &list)
	if len(list.Items) != 2 {
		t.Errorf("a second import duplicated documents: %d", len(list.Items))
	}

	// A damaged file is reported, not imported.
	for _, f := range files {
		if strings.HasSuffix(f, ".pdf") && strings.Contains(f, "other - ") {
			_ = os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("corrupted"), 0o640)
		}
	}
	fresh := newEnv(t)
	rep, err = exchange.Import(fresh.ctx, fresh.a.Pool, fresh.a.Store, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Documents != 1 || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "checksum") {
		t.Errorf("damaged export: %+v", rep)
	}
	// Not an export at all.
	if _, err := exchange.Import(fresh.ctx, fresh.a.Pool, fresh.a.Store, t.TempDir()); err == nil {
		t.Error("an empty folder should be rejected")
	}
}

func TestFacetCounts(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var tag, corr struct{ ID string }
	c.do("POST", "/api/v1/tags", map[string]any{"space_id": e.family, "name": "Bills"}, 201, &tag)
	c.do("POST", "/api/v1/correspondents", map[string]any{"space_id": e.family, "name": "Power Co"}, 201, &corr)
	for i, txt := range []string{"Electricity bill for the month of January 2026", "Electricity bill for the month of February 2026", "Holiday photos album description text"} {
		d := c.upload(e.family, fmt.Sprintf("f%d.pdf", i), minimalPDF(txt))
		c.waitStatus(d.ID, "ready")
		if i < 2 {
			c.do("PATCH", "/api/v1/documents/"+d.ID, map[string]any{"tag_ids": []string{tag.ID}, "correspondent_id": corr.ID}, 200, nil)
		}
	}
	var res struct {
		Total  int `json:"total"`
		Facets struct {
			Tags, Correspondents, Statuses map[string]int
		} `json:"facets"`
	}
	c.do("GET", "/api/v1/documents?facets=true", nil, 200, &res)
	if res.Total != 3 || res.Facets.Tags[tag.ID] != 2 || res.Facets.Correspondents[corr.ID] != 2 || res.Facets.Statuses["ready"] != 3 {
		t.Fatalf("facets: %+v", res)
	}
	res.Facets.Tags, res.Facets.Correspondents, res.Facets.Statuses = nil, nil, nil
	c.do("GET", "/api/v1/documents?facets=true&q=holiday", nil, 200, &res)
	if res.Total != 1 || len(res.Facets.Tags) != 0 || res.Facets.Statuses["ready"] != 1 {
		t.Fatalf("filtered facets: %+v", res)
	}
}
