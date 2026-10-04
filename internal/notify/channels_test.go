package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/smtp"
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
	if _, err := newHTTPClient(false).Get(srv.URL); err == nil || !strings.Contains(err.Error(), "local network") {
		t.Fatalf("loopback request not blocked: %v", err)
	}
	resp, err := newHTTPClient(true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowLocal request failed: %v", err)
	}
	resp.Body.Close()
}

// Security "none" (a relay or proxy on the local network) must still log in: Go's own
// smtp.PlainAuth refuses to send the password over an unencrypted connection.
func TestPlainAuthWithoutTLS(t *testing.T) {
	info := &smtp.ServerInfo{Name: "mail.example.com", TLS: false, Auth: []string{"PLAIN"}}
	if _, _, err := smtp.PlainAuth("", "u", "pw", info.Name).Start(info); err == nil {
		t.Fatal("expected the standard library to refuse; this test no longer proves anything")
	}
	proto, resp, err := plainAuth{"u", "pw"}.Start(info)
	if err != nil || proto != "PLAIN" || string(resp) != "\x00u\x00pw" {
		t.Fatalf("got %q %q %v", proto, resp, err)
	}
}

func TestLocalTargetsByContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL, nil)
	if err := do(req); err == nil {
		t.Fatal("a plain delivery reached a loopback address")
	}
	req, _ = http.NewRequestWithContext(WithLocalTargets(req.Context(), true), "POST", srv.URL, nil)
	if err := do(req); err != nil {
		t.Fatalf("an allowed delivery failed: %v", err)
	}
}

// Microsoft 365 only offers AUTH LOGIN: the server asks for the username, then the password.
func TestLoginAuth(t *testing.T) {
	a := loginAuth{"me@example.com", "pw"}
	if proto, resp, err := a.Start(&smtp.ServerInfo{Name: "smtp.office365.com", TLS: true}); proto != "LOGIN" || resp != nil || err != nil {
		t.Fatalf("start: %q %q %v", proto, resp, err)
	}
	for challenge, want := range map[string]string{"Username:": "me@example.com", "Password:": "pw"} {
		if got, err := a.Next([]byte(challenge), true); err != nil || string(got) != want {
			t.Errorf("%s: got %q, %v", challenge, got, err)
		}
	}
	if _, err := a.Next([]byte("Something else"), true); err == nil {
		t.Error("an unknown challenge was answered")
	}
}

func TestNtfyJSON(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	ctx := WithLocalTargets(context.Background(), true)
	if err := sendNtfy(ctx, NtfyConfig{Server: srv.URL, Topic: "docs"}, Message{Title: "बिजली का बिल", Body: "ready", URL: "https://d.example/x"}); err != nil {
		t.Fatal(err)
	}
	if got["topic"] != "docs" || got["title"] != "बिजली का बिल" || got["click"] != "https://d.example/x" {
		t.Fatalf("payload %v", got)
	}
}
