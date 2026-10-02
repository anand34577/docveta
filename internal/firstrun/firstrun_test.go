package firstrun

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFormURL(t *testing.T) {
	u, err := Form{Host: "db.local", Port: "5432", User: "docveta", Password: "p@ss w/rd", Database: "docveta", SSLMode: "require"}.URL()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(u)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "db.local" || cfg.User != "docveta" || cfg.Password != "p@ss w/rd" || cfg.Database != "docveta" {
		t.Errorf("round trip: %+v", cfg)
	}
	if _, err := (Form{Host: "h", Port: "x", User: "u", Database: "d"}).URL(); err == nil {
		t.Error("bad port accepted")
	}
}

func TestExplainConnectionRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Check(ctx, "postgres://u:p@127.0.0.1:1/db?sslmode=disable", false)
	if err == nil || !strings.Contains(err.Error(), "Is PostgreSQL installed and running") {
		t.Fatalf("got %v", err)
	}
}

func TestRequestChecks(t *testing.T) {
	r := httptest.NewRequest("POST", "http://localhost:8080/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if !isLocal(r) {
		t.Error("loopback not local")
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if isLocal(r) {
		t.Error("proxied request treated as local")
	}
	r2 := httptest.NewRequest("POST", "http://192.168.1.5:8080/", nil)
	r2.RemoteAddr = "192.168.1.20:5555"
	if isLocal(r2) {
		t.Error("LAN request treated as local")
	}

	for _, c := range []struct {
		site, origin string
		want         bool
	}{{"same-origin", "", true}, {"cross-site", "", false}, {"", "http://localhost:8080", true}, {"", "https://evil.example", false}, {"", "", true}} {
		r := httptest.NewRequest("POST", "http://localhost:8080/", nil)
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := sameOrigin(r); got != c.want {
			t.Errorf("site=%q origin=%q: %v", c.site, c.origin, got)
		}
	}
}
