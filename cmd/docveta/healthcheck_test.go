package main

import "testing"

func TestHealthURL(t *testing.T) {
	for in, want := range map[string]string{
		":8080":            "http://localhost:8080/healthz",
		"0.0.0.0:8080":     "http://localhost:8080/healthz",
		"[::]:9000":        "http://localhost:9000/healthz",
		"127.0.0.1:18080":  "http://127.0.0.1:18080/healthz",
		"192.168.1.5:8080": "http://192.168.1.5:8080/healthz",
		"[::1]:8080":       "http://[::1]:8080/healthz",
	} {
		if got, err := healthURL(in); err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("an address without a port was accepted")
	}
}
