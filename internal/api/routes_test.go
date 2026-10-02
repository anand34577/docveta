package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anand34577/docveta/internal/platform/config"
)

// Registering all routes panics if any two patterns conflict.
func TestRoutesRegisterWithoutConflicts(t *testing.T) {
	u, _ := url.Parse("https://docs.example.com")
	a := New(Deps{Cfg: &config.Config{BaseURL: u}})
	mux := http.NewServeMux()
	a.Register(mux)

	// Unknown API and worker paths return JSON 404, not the web app.
	for _, p := range []string{"/api/v1/nope", "/worker/v1/nope"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "problem+json") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	// Authenticated endpoints reject anonymous requests.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/documents", nil))
	if rec.Code != 401 {
		t.Errorf("anonymous list: %d", rec.Code)
	}
	if a.cookieName != "__Host-docveta_session" {
		t.Errorf("https must use __Host- cookie, got %s", a.cookieName)
	}
}

func TestSameOrigin(t *testing.T) {
	u, _ := url.Parse("https://docs.example.com")
	a := New(Deps{Cfg: &config.Config{BaseURL: u}})
	cases := []struct {
		headers map[string]string
		want    bool
	}{
		{map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{map[string]string{"Origin": "https://docs.example.com"}, true},
		{map[string]string{"Origin": "https://evil.example"}, false},
		{map[string]string{}, true}, // non-browser client
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/v1/x", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := a.sameOrigin(r); got != c.want {
			t.Errorf("%v: got %v", c.headers, got)
		}
	}
}
