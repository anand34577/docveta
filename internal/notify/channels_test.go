package notify

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestBlockedAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "192.168.1.5": true, "172.16.0.1": true,
		"169.254.169.254": true, "100.100.1.1": true, "::1": true, "fe80::1": true,
		"fd00::1": true, "::ffff:127.0.0.1": true, "0.0.0.0": true,
		"8.8.8.8": false, "2606:4700::1111": false, "100.128.0.1": false,
	} {
		if got := blockedAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("blockedAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestHTTPClientGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	if _, err := newHTTPClient(false).Get(srv.URL); err == nil || !strings.Contains(err.Error(), "DOCVETA_ALLOW_LOCAL_TARGETS") {
		t.Fatalf("loopback request not blocked: %v", err)
	}
	resp, err := newHTTPClient(true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowLocal request failed: %v", err)
	}
	resp.Body.Close()
}
