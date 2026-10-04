package app

import (
	"os"
	"strings"
	"testing"
)

func TestLocalURL(t *testing.T) {
	for in, want := range map[string]string{
		":8080": "http://127.0.0.1:8080", "0.0.0.0:9000": "http://127.0.0.1:9000", "[::]:8080": "http://127.0.0.1:8080",
		"192.168.1.5:8080": "http://192.168.1.5:8080", "bad": "http://127.0.0.1:8080",
	} {
		if got := localURL(in); got != want {
			t.Errorf("localURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChildEnvDropsSecrets(t *testing.T) {
	t.Setenv("DOCVETA_SECRET_KEY", "s")
	t.Setenv("DOCVETA_DATABASE_URL", "postgres://x")
	t.Setenv("DOCVETA_OCR_DEVICE", "gpu")
	env := strings.Join(childEnv(), "\n")
	if strings.Contains(env, "DOCVETA_SECRET_KEY=") || strings.Contains(env, "DOCVETA_DATABASE_URL=") || !strings.Contains(env, "DOCVETA_OCR_DEVICE=gpu") {
		t.Errorf("child env: %s", env)
	}
}

func TestFindLocalOCR(t *testing.T) {
	if findLocalOCR("off") != "" {
		t.Error("off should disable")
	}
	if p := findLocalOCR(os.Args[0]); p != os.Args[0] {
		t.Errorf("explicit path: %q", p)
	}
	if p := findLocalOCR("auto"); p != "" { // nothing next to the test binary
		t.Errorf("auto found %q", p)
	}
}

func TestMetricsAccess(t *testing.T) {
	for ip, want := range map[string]bool{"127.0.0.1": true, "192.168.1.9": true, "10.2.3.4": true, "::1": true, "fd00::2": true, "8.8.8.8": false, "": false} {
		if got := localClient(ip); got != want {
			t.Errorf("%q: got %v", ip, got)
		}
	}
}
