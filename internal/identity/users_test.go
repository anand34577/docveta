package identity

import "testing"

func TestValidTimezone(t *testing.T) {
	for tz, want := range map[string]bool{"Asia/Kolkata": true, "Europe/Berlin": true, "UTC": true,
		"": false, "Local": false, "Mars/Olympus": false, "../etc/passwd": false} {
		if got := validTimezone(tz); got != want {
			t.Errorf("validTimezone(%q) = %v, want %v", tz, got, want)
		}
	}
}
