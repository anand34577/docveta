package notify

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
)

// Prefs are a user's notification preferences. Quiet hours hold back push messages
// (Gotify, ntfy, email, ...) for non-urgent events until the quiet period ends; the
// in-app inbox always updates immediately.
type Prefs struct {
	QuietEnabled bool   `json:"quiet_enabled"`
	QuietStart   string `json:"quiet_start"` // "22:00", in the user's time zone
	QuietEnd     string `json:"quiet_end"`   // "07:00"
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (s *Service) Prefs(ctx context.Context, userID uuid.UUID) (Prefs, error) {
	p := Prefs{QuietStart: "22:00", QuietEnd: "07:00"}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT notify_prefs FROM users WHERE id=$1`, userID).Scan(&raw)
	if db.IsNoRows(err) {
		return p, apperr.NotFound("User")
	}
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal(raw, &p)
	return p, nil
}

func (s *Service) SetPrefs(ctx context.Context, p *auth.Principal, in Prefs) (Prefs, error) {
	var v apperr.Validation
	if !hhmm.MatchString(in.QuietStart) {
		v.Add("quiet_start", "Use a time like 22:00")
	}
	if !hhmm.MatchString(in.QuietEnd) {
		v.Add("quiet_end", "Use a time like 07:00")
	}
	if in.QuietEnabled && in.QuietStart == in.QuietEnd {
		v.Add("quiet_end", "Quiet hours can't start and end at the same time")
	}
	if err := v.Err(); err != nil {
		return Prefs{}, err
	}
	raw, _ := json.Marshal(in)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET notify_prefs=$2 WHERE id=$1`, p.UserID, raw); err != nil {
		return Prefs{}, err
	}
	return in, nil
}

// QuietUntil returns when the quiet period that contains now ends, or the zero time if
// now is outside it (or quiet hours are off). The window may wrap past midnight.
func (p Prefs) QuietUntil(now time.Time, loc *time.Location) time.Time {
	if !p.QuietEnabled || !hhmm.MatchString(p.QuietStart) || !hhmm.MatchString(p.QuietEnd) {
		return time.Time{}
	}
	local := now.In(loc)
	at := func(hm string, dayOffset int) time.Time {
		t, _ := time.Parse("15:04", hm)
		return time.Date(local.Year(), local.Month(), local.Day()+dayOffset, t.Hour(), t.Minute(), 0, 0, loc)
	}
	start, end := at(p.QuietStart, 0), at(p.QuietEnd, 0)
	if !end.After(start) { // wraps midnight, e.g. 22:00 → 07:00
		if !local.Before(start) { // evening: ends tomorrow morning
			return at(p.QuietEnd, 1)
		}
		if local.Before(end) { // early morning: ends this morning
			return end
		}
		return time.Time{}
	}
	if !local.Before(start) && local.Before(end) {
		return end
	}
	return time.Time{}
}
