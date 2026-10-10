package app

import (
	"archive/zip"
	"bytes"
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
