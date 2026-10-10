package httpx

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func compressed(t *testing.T, h http.HandlerFunc, edit func(*http.Request)) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	if edit != nil {
		edit(req)
	}
	rec := httptest.NewRecorder()
	Compress()(h).ServeHTTP(rec, req)
	return rec.Result()
}

func TestCompress(t *testing.T) {
	big := strings.Repeat(`{"title":"Electricity bill"},`, 200)
	jsonBody := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, big)
	}

	res := compressed(t, jsonBody, nil)
	if res.StatusCode != http.StatusCreated || res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Content-Length") != "" {
		t.Fatalf("JSON should be gzipped: %d %v", res.StatusCode, res.Header)
	}
	if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary: %q", res.Header.Get("Vary"))
	}
	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != big {
		t.Fatalf("body changed: %d bytes, want %d", len(got), len(big))
	}

	plain := func(name string, res *http.Response, want string) {
		t.Helper()
		b, _ := io.ReadAll(res.Body)
		if res.Header.Get("Content-Encoding") != "" || string(b) != want {
			t.Fatalf("%s should pass through untouched: %v, %d bytes", name, res.Header, len(b))
		}
	}
	plain("no Accept-Encoding", compressed(t, jsonBody, func(r *http.Request) { r.Header.Del("Accept-Encoding") }), big)
	plain("gzip refused", compressed(t, jsonBody, func(r *http.Request) { r.Header.Set("Accept-Encoding", "br, gzip;q=0") }), big)
	plain("Range request", compressed(t, jsonBody, func(r *http.Request) { r.Header.Set("Range", "bytes=0-9") }), big)
	plain("a PDF", compressed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		io.WriteString(w, big)
	}, nil), big)
	plain("short with a known length", compressed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "2")
		io.WriteString(w, "ok")
	}, nil), "ok")
	// Something a handler encoded itself isn't encoded twice.
	res = compressed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "br")
		io.WriteString(w, big)
	}, nil)
	if b, _ := io.ReadAll(res.Body); res.Header.Get("Content-Encoding") != "br" || string(b) != big {
		t.Fatalf("already encoded: %v, %d bytes", res.Header, len(b))
	}

	// Event streams are flushed message by message and must not sit in a gzip buffer.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	Compress()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: 1\n\n")
		w.(http.Flusher).Flush()
	})).ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "data: 1\n\n" || !rec.Flushed {
		t.Fatalf("event stream: %v %q flushed=%v", rec.Header(), rec.Body.String(), rec.Flushed)
	}

	// No body, no encoding header (a 204 or 304 with Content-Encoding confuses clients).
	res = compressed(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	}, nil)
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("204: %v", res.Header)
	}

	// A type sniffed from the body (plain w.Write of HTML) is compressed too, and a flushed
	// compressed response is readable up to the flush.
	res = compressed(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "<!doctype html><title>Docveta</title>"+strings.Repeat("<p>x</p>", 300))
		w.(http.Flusher).Flush()
	}, nil)
	if res.Header.Get("Content-Encoding") != "gzip" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("sniffed HTML: %v", res.Header)
	}
	if zr, err = gzip.NewReader(res.Body); err != nil {
		t.Fatal(err)
	}
	if got, err = io.ReadAll(zr); err != nil || !strings.HasPrefix(string(got), "<!doctype html>") {
		t.Fatalf("sniffed HTML body: %v %q", err, got[:min(len(got), 20)])
	}
}
