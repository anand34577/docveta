package app

import (
	"github.com/anand34577/docveta/internal/search"

	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// The default list (newest added first) reads each space's newest documents and merges them.
// Paging through it must give every document once, in order, whatever the page size.
func TestNewestFirstAcrossSpaces(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var sp struct {
		Items []struct{ ID, Kind string }
	}
	c.do("GET", "/api/v1/spaces", nil, 200, &sp)
	personal := ""
	for _, s := range sp.Items {
		if s.Kind == "personal" {
			personal = s.ID
		}
	}
	if personal == "" {
		t.Fatalf("no personal space in %+v", sp.Items)
	}
	var uploaded []string
	for i := range 7 {
		space := e.family
		if i%3 == 0 {
			space = personal
		}
		uploaded = append(uploaded, c.upload(space, fmt.Sprintf("paper%d.pdf", i), minimalPDF(fmt.Sprintf("Paper number %d about the water supply", i))).ID)
	}
	newest := slices.Clone(uploaded)
	slices.Reverse(newest)

	walk := func(query string) []string {
		t.Helper()
		var got []string
		cursor := ""
		for range 10 {
			var page struct {
				Items []struct{ ID string }
				Next  *string `json:"next_cursor"`
				Total *int    `json:"total"`
			}
			c.do("GET", "/api/v1/documents?limit=3&"+query+cursor, nil, 200, &page)
			if cursor == "" && (page.Total == nil || *page.Total != len(uploaded)) {
				t.Fatalf("%s: total %v, want %d", query, page.Total, len(uploaded))
			}
			for _, d := range page.Items {
				got = append(got, d.ID)
			}
			if page.Next == nil {
				return got
			}
			cursor = "&cursor=" + url.QueryEscape(*page.Next)
		}
		t.Fatalf("%s: paging never ended: %v", query, got)
		return nil
	}
	if got := walk(""); !slices.Equal(got, newest) {
		t.Fatalf("newest first:\n got %v\nwant %v", got, newest)
	}
	if got := walk("sort=added"); !slices.Equal(got, uploaded) {
		t.Fatalf("oldest first:\n got %v\nwant %v", got, uploaded)
	}
	if got := walk("inbox=true&sort=-added"); !slices.Equal(got, newest) { // with a filter on top
		t.Fatalf("inbox:\n got %v\nwant %v", got, newest)
	}
	// One space only, and words to search for with an explicit date order.
	var one struct{ Items []struct{ ID string } }
	c.do("GET", "/api/v1/documents?space_id="+personal, nil, 200, &one)
	if len(one.Items) != 3 || one.Items[0].ID != uploaded[6] || one.Items[2].ID != uploaded[0] {
		t.Fatalf("personal space: %+v", one.Items)
	}
	c.do("GET", "/api/v1/documents?sort=-added&q=paper", nil, 200, &one)
	if len(one.Items) != len(uploaded) || one.Items[0].ID != newest[0] {
		t.Fatalf("search by date added: %+v", one.Items)
	}
	// A cursor that isn't one of ours is refused, not passed to the database.
	c.raw("GET", "/api/v1/documents?cursor=eyJrIjoibm90LWEtZGF0ZSIsImkiOiJ4In0", nil, nil, 422)
}

// Every other order also merges each space's own index: paging through it gives every
// document once, undated documents last when newest first, and stale or damaged cursors are
// refused.
func TestSortOrdersAcrossSpaces(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var me struct {
		Spaces []struct{ ID, Kind string } `json:"spaces"`
	}
	c.do("GET", "/api/v1/me", nil, 200, &me)
	personal := ""
	for _, s := range me.Spaces {
		if s.Kind == "personal" {
			personal = s.ID
		}
	}
	type doc struct{ id, title, date string }
	dates := []string{"2024-05-01", "", "2021-01-31", "2024-05-01", "", "2019-12-25", "2023-07-04"}
	titles := []string{"delta", "Alpha", "charlie", "bravo", "echo", "Foxtrot", "alpha"}
	var docs []doc
	for i := range dates {
		space := e.family
		if i%2 == 0 {
			space = personal
		}
		d := c.upload(space, fmt.Sprintf("sorted%d.pdf", i), minimalPDF(fmt.Sprintf("Sorted paper %d", i)))
		patch := map[string]any{"title": titles[i]}
		if dates[i] != "" {
			patch["document_date"] = dates[i]
		}
		c.do("PATCH", "/api/v1/documents/"+d.ID, patch, 200, nil)
		docs = append(docs, doc{d.ID, titles[i], dates[i]})
	}
	walk := func(sort string) []string {
		t.Helper()
		var got []string
		cursor := ""
		for range 10 {
			var page struct {
				Items []struct{ ID string }
				Next  *string `json:"next_cursor"`
			}
			c.do("GET", "/api/v1/documents?limit=2&sort="+sort+cursor, nil, 200, &page)
			for _, d := range page.Items {
				got = append(got, d.ID)
			}
			if page.Next == nil {
				return got
			}
			cursor = "&cursor=" + url.QueryEscape(*page.Next)
		}
		t.Fatalf("%s: paging never ended: %v", sort, got)
		return nil
	}
	ids := func(cmp func(a, b doc) int) []string {
		s := slices.Clone(docs)
		slices.SortStableFunc(s, func(a, b doc) int {
			if r := cmp(a, b); r != 0 {
				return r
			}
			return strings.Compare(a.id, b.id) // uuids compare as their text does
		})
		out := make([]string, len(s))
		for i, d := range s {
			out[i] = d.id
		}
		return out
	}
	byDate := ids(func(a, b doc) int { return strings.Compare(a.date, b.date) }) // "" (undated) first
	byTitle := ids(func(a, b doc) int { return strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title)) })
	newestDate := slices.Clone(byDate)
	slices.Reverse(newestDate)
	titleZA := slices.Clone(byTitle)
	slices.Reverse(titleZA)
	for sort, want := range map[string][]string{"date": byDate, "-date": newestDate, "title": byTitle, "-title": titleZA} {
		if got := walk(sort); !slices.Equal(got, want) {
			t.Errorf("sort=%s:\n got %v\nwant %v", sort, got, want)
		}
	}
	// The last change first: the last document edited leads.
	c.do("PATCH", "/api/v1/documents/"+docs[2].id, map[string]any{"title": "charlie again"}, 200, nil)
	if got := walk("-updated"); len(got) != len(docs) || got[0] != docs[2].id {
		t.Errorf("sort=-updated: %v", got)
	}
	// Cursors from before undated documents sorted as -infinity still work; a damaged date doesn't reach the database.
	old := base64.RawURLEncoding.EncodeToString([]byte(`{"k":"0000-00-00","i":"` + newestDate[len(newestDate)-1] + `"}`))
	c.do("GET", "/api/v1/documents?sort=-date&cursor="+old, nil, 200, nil)
	bad := base64.RawURLEncoding.EncodeToString([]byte(`{"k":"2024-13-45","i":"` + docs[0].id + `"}`))
	c.raw("GET", "/api/v1/documents?sort=date&cursor="+bad, nil, nil, 422)
}

// The counts in the sidebar and on Home follow uploads and the Trash at once, although the
// totals are cached between changes.
func TestCountsFollowChanges(t *testing.T) {
	e := newEnv(t)
	c := e.c
	type stats struct{ Total, Trash int }
	count := func() (st stats, family int) {
		t.Helper()
		c.do("GET", "/api/v1/documents/stats", nil, 200, &st)
		var me struct {
			Spaces []struct {
				ID    string
				Count int `json:"document_count"`
			} `json:"spaces"`
		}
		c.do("GET", "/api/v1/me", nil, 200, &me)
		for _, s := range me.Spaces {
			if s.ID == e.family {
				family = s.Count
			}
		}
		return st, family
	}
	before, fam := count()
	d := c.upload(e.family, "counted.pdf", minimalPDF("Counted paper"))
	if st, f := count(); st.Total != before.Total+1 || f != fam+1 {
		t.Fatalf("after upload: %+v, family %d (before %+v, %d)", st, f, before, fam)
	}
	c.do("DELETE", "/api/v1/documents/"+d.ID, nil, 204, nil)
	if st, f := count(); st.Total != before.Total || st.Trash != before.Trash+1 || f != fam {
		t.Fatalf("after trash: %+v, family %d", st, f)
	}
	c.do("POST", "/api/v1/documents/"+d.ID+"/restore", nil, 204, nil)
	if st, f := count(); st.Total != before.Total+1 || f != fam+1 {
		t.Fatalf("after restore: %+v, family %d", st, f)
	}
}

func TestDownloadAsZip(t *testing.T) {
	e := newEnv(t)
	c := e.c
	first, second := minimalPDF("Rent receipt for January"), minimalPDF("Rent receipt for February")
	a := c.upload(e.family, "receipt.pdf", first)
	b := c.upload(e.family, "receipt.pdf", second) // the same name twice must not overwrite in the ZIP
	missing := "00000000-0000-4000-8000-000000000001"

	read := func(body []byte) map[string][]byte {
		t.Helper()
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("not a ZIP: %v: %.80q", err, body)
		}
		out := map[string][]byte{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			out[f.Name], _ = io.ReadAll(rc)
			rc.Close()
		}
		return out
	}

	files := read(c.raw("POST", "/api/v1/documents/archive", bytes.NewReader(mustJSON(map[string]any{"ids": []string{a.ID, b.ID, missing, a.ID}})),
		map[string]string{"Content-Type": "application/json"}, 200))
	if len(files) != 3 || !bytes.Equal(files["receipt.pdf"], first) || !bytes.Equal(files["receipt (2).pdf"], second) {
		t.Fatalf("ZIP holds %v", keys(files))
	}
	if note := string(files["Not included.txt"]); !strings.HasPrefix(note, "1 of the 3 documents") {
		t.Fatalf("note about the missing document: %q", note)
	}

	// The form a browser posts, so the download goes straight to disk.
	form := url.Values{"ids": {a.ID + "," + b.ID}, "kind": {"archive"}}
	files = read(c.raw("POST", "/api/v1/documents/archive", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 200))
	if len(files) != 2 {
		t.Fatalf("form post: %v", keys(files))
	}

	c.do("POST", "/api/v1/documents/archive", map[string]any{"ids": []string{}}, 422, nil)
	c.do("POST", "/api/v1/documents/archive", map[string]any{"ids": []string{"not-an-id"}}, 422, nil)
	c.do("POST", "/api/v1/documents/archive", map[string]any{"ids": []string{a.ID}, "kind": "thumbnail"}, 422, nil)
	c.do("POST", "/api/v1/documents/archive", map[string]any{"ids": []string{missing}}, 404, nil)

	// Someone outside the space gets nothing of it.
	c.do("POST", "/api/v1/admin/users", map[string]any{"email": "kid@example.com", "display_name": "Kid", "password": "another-long-pw"}, 201, nil)
	other := newClient(t, e.srv.URL)
	other.do("POST", "/api/v1/auth/login", map[string]any{"email": "kid@example.com", "password": "another-long-pw"}, 200, nil)
	other.do("POST", "/api/v1/documents/archive", map[string]any{"ids": []string{a.ID, b.ID}}, 404, nil)
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Search as you type: words starting with what was typed, newest first, from every space;
// a misspelt title still turns up; the Trash doesn't.
func TestSuggestions(t *testing.T) {
	e := newEnv(t)
	c := e.c
	var me struct {
		Spaces []struct{ ID, Kind string } `json:"spaces"`
	}
	c.do("GET", "/api/v1/me", nil, 200, &me)
	personal := ""
	for _, s := range me.Spaces {
		if s.Kind == "personal" {
			personal = s.ID
		}
	}
	older := c.upload(personal, "a.pdf", minimalPDF("first"))
	c.do("PATCH", "/api/v1/documents/"+older.ID, map[string]any{"title": "Electricity bill March"}, 200, nil)
	newer := c.upload(e.family, "b.pdf", minimalPDF("second"))
	c.do("PATCH", "/api/v1/documents/"+newer.ID, map[string]any{"title": "Electricity bill April"}, 200, nil)
	trashed := c.upload(e.family, "c.pdf", minimalPDF("third"))
	c.do("PATCH", "/api/v1/documents/"+trashed.ID, map[string]any{"title": "Electricity meter photo"}, 200, nil)
	c.do("DELETE", "/api/v1/documents/"+trashed.ID, nil, 204, nil)

	suggest := func(q string) []string {
		t.Helper()
		var res struct{ Items []struct{ ID, Title string } }
		c.do("GET", "/api/v1/documents/suggest?q="+url.QueryEscape(q), nil, 200, &res)
		var ids []string
		for _, x := range res.Items {
			ids = append(ids, x.ID)
		}
		return ids
	}
	if got := suggest("electr"); !slices.Equal(got, []string{newer.ID, older.ID}) {
		t.Errorf("electr: %v, want newest first %v", got, []string{newer.ID, older.ID})
	}
	if got := suggest("electricity mar"); len(got) == 0 || got[0] != older.ID { // look-alike titles may follow
		t.Errorf("electricity mar: %v", got)
	}
	if got := suggest("elektricity"); !slices.Contains(got, older.ID) || !slices.Contains(got, newer.ID) || slices.Contains(got, trashed.ID) {
		t.Errorf("misspelt: %v", got)
	}
	if got := suggest("zzqx"); len(got) != 0 {
		t.Errorf("nothing like it: %v", got)
	}
}

// Administration → Audit log: a filter is a prefix of the action, taken literally, and paging
// through a filtered log goes back in time.
func TestAuditLogFilter(t *testing.T) {
	e := newEnv(t)
	for i := range 3 {
		if _, err := e.a.Pool.Exec(e.ctx, `INSERT INTO audit_log (actor_type, action, details) VALUES ('system', $1, '{}')`,
			[]string{"share.create", "share.revoke", "share%x"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Items []struct {
			ID     int64
			Action string
		}
	}
	get := func(q string) []string {
		t.Helper()
		var p page
		e.c.do("GET", "/api/v1/admin/audit?"+q, nil, 200, &p)
		var out []string
		for _, x := range p.Items {
			out = append(out, x.Action)
		}
		return out
	}
	if got := get("action=share"); !slices.Equal(got, []string{"share%x", "share.revoke", "share.create"}) {
		t.Errorf("share: %v", got)
	}
	if got := get("action=share.revoke"); !slices.Equal(got, []string{"share.revoke"}) {
		t.Errorf("share.revoke: %v", got)
	}
	if got := get("action=" + url.QueryEscape("share%")); !slices.Equal(got, []string{"share%x"}) {
		t.Errorf("a %% is a percent sign, not a wildcard: %v", got)
	}
	var first page
	e.c.do("GET", "/api/v1/admin/audit?action=share&limit=1", nil, 200, &first)
	if len(first.Items) != 1 {
		t.Fatalf("limit 1: %+v", first)
	}
	if got := get(fmt.Sprintf("action=share&before=%d", first.Items[0].ID)); !slices.Equal(got, []string{"share.revoke", "share.create"}) {
		t.Errorf("older ones: %v", got)
	}
}

// A word in most of a large library: best match ranks only the newest matches and the count
// stops there, while sorting by date still reaches every document. The limits are lowered so a
// handful of documents is "large".
func TestBroadSearch(t *testing.T) {
	broadMin, relevanceCap := search.BroadMin, search.RelevanceCap
	search.BroadMin, search.RelevanceCap = 3, 4
	t.Cleanup(func() { search.BroadMin, search.RelevanceCap = broadMin, relevanceCap })
	e := newEnv(t)
	c := e.c
	var matching []string // oldest first
	for i := range 7 {
		d := c.upload(e.family, fmt.Sprintf("h%d.pdf", i), minimalPDF(fmt.Sprintf("Household paper number %d", i)))
		c.waitStatus(d.ID, "ready")
		matching = append(matching, d.ID)
	}
	other := c.upload(e.family, "o.pdf", minimalPDF("Something else entirely"))
	c.waitStatus(other.ID, "ready")

	type page struct {
		Items  []struct{ ID string }
		Total  *int    `json:"total"`
		Capped bool    `json:"total_capped"`
		Next   *string `json:"next_cursor"`
	}
	walk := func(query string) (ids []string, first page) {
		t.Helper()
		cursor := ""
		for range 10 {
			var p page
			c.do("GET", "/api/v1/documents?limit=2&"+query+cursor, nil, 200, &p)
			if cursor == "" {
				first = p
			}
			for _, d := range p.Items {
				ids = append(ids, d.ID)
			}
			if p.Next == nil {
				return ids, first
			}
			cursor = "&cursor=" + url.QueryEscape(*p.Next)
		}
		t.Fatalf("%s: paging never ended", query)
		return nil, first
	}
	got, first := walk("q=household")
	newest := slices.Clone(matching[len(matching)-4:])
	sortedGot := slices.Clone(got)
	slices.Sort(sortedGot)
	slices.Sort(newest)
	if !slices.Equal(sortedGot, newest) {
		t.Errorf("best match over a word in most documents: got %v, want the 4 newest %v", got, newest)
	}
	if first.Total == nil || *first.Total != 4 || !first.Capped {
		t.Errorf("count: %v capped=%v, want 4+", first.Total, first.Capped)
	}
	all, _ := walk("q=household&sort=-added")
	if len(all) != len(matching) {
		t.Errorf("newest first reaches %d of %d documents", len(all), len(matching))
	}
	// A word in few documents is ranked as before.
	if one, f := walk("q=entirely"); len(one) != 1 || one[0] != other.ID || f.Capped {
		t.Errorf("narrow word: %v", one)
	}
}
