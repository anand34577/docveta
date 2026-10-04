package api

import (
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"

	"github.com/anand34577/docveta/internal/platform/config"
)

func TestPublicBase(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32")}
	cases := []struct {
		name, base, host, remote, fwdHost, fwdProto, want string
	}{
		{"configured address wins", "https://docs.example.com", "192.168.1.5:8080", "192.168.1.9:5000", "", "", "https://docs.example.com"},
		{"installer default, LAN address", "http://localhost:8080", "192.168.1.5:8080", "192.168.1.9:5000", "", "", "http://192.168.1.5:8080"},
		{"installer default, opened on the server", "http://localhost:8080", "localhost:8080", "127.0.0.1:5000", "", "", "http://localhost:8080"},
		{"trusted proxy", "http://localhost:8080", "127.0.0.1:8080", "10.0.0.1:5000", "docs.home.lan", "https", "https://docs.home.lan"},
		{"untrusted proxy headers ignored", "http://localhost:8080", "192.168.1.5:8080", "192.168.1.9:5000", "evil.example", "https", "http://192.168.1.5:8080"},
		{"junk host header", "http://localhost:8080", "a b/c", "192.168.1.9:5000", "", "", "http://localhost:8080"},
		{"IPv6", "http://127.0.0.1:8080", "[fd00::5]:8080", "[fd00::9]:5000", "", "", "http://[fd00::5]:8080"},
	}
	for _, c := range cases {
		u, _ := url.Parse(c.base)
		a := New(Deps{Cfg: &config.Config{BaseURL: u, TrustedProxies: proxy}})
		r := httptest.NewRequest("GET", "/", nil)
		r.Host, r.RemoteAddr = c.host, c.remote
		if c.fwdHost != "" {
			r.Header.Set("X-Forwarded-Host", c.fwdHost)
			r.Header.Set("X-Forwarded-Proto", c.fwdProto)
		}
		if got := a.publicBase(r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if o := originOf("https://docs.example.com/sub"); o != "https://docs.example.com" {
		t.Errorf("originOf: %q", o)
	}
}
