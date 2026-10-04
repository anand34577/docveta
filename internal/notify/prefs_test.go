package notify

import (
	"testing"
	"time"
)

func TestQuietUntil(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	night := Prefs{QuietEnabled: true, QuietStart: "22:00", QuietEnd: "07:00"}
	day := Prefs{QuietEnabled: true, QuietStart: "13:00", QuietEnd: "14:30"}
	at := func(s string) time.Time {
		tm, err := time.ParseInLocation("2006-01-02 15:04", s, ist)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	cases := []struct {
		name string
		p    Prefs
		now  string
		want string // "" = not quiet
	}{
		{"evening before midnight", night, "2026-10-03 23:15", "2026-10-04 07:00"},
		{"after midnight", night, "2026-10-04 02:00", "2026-10-04 07:00"},
		{"exactly at start", night, "2026-10-03 22:00", "2026-10-04 07:00"},
		{"exactly at end", night, "2026-10-04 07:00", ""},
		{"midday is not quiet", night, "2026-10-03 12:00", ""},
		{"same-day window inside", day, "2026-10-03 13:30", "2026-10-03 14:30"},
		{"same-day window outside", day, "2026-10-03 15:00", ""},
		{"disabled", Prefs{QuietStart: "22:00", QuietEnd: "07:00"}, "2026-10-03 23:00", ""},
		{"garbage times are ignored", Prefs{QuietEnabled: true, QuietStart: "late", QuietEnd: "early"}, "2026-10-03 23:00", ""},
	}
	for _, c := range cases {
		got := c.p.QuietUntil(at(c.now), ist)
		if c.want == "" {
			if !got.IsZero() {
				t.Errorf("%s: want not quiet, got %v", c.name, got)
			}
			continue
		}
		if !got.Equal(at(c.want)) {
			t.Errorf("%s: got %v, want %s", c.name, got, c.want)
		}
	}
	// The same instant is quiet in India (23:00) but not in London (17:30).
	london, _ := time.LoadLocation("Europe/London")
	if !night.QuietUntil(at("2026-10-03 23:00"), london).IsZero() {
		t.Error("17:30 in London shouldn't be quiet")
	}
}
