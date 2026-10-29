package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/jobs"
)

// sendDigests sends each active person a summary of their week once their week has reached
// Monday 9:00 in their time zone: documents added, waiting in the Inbox, failed, and dates
// coming up. Weeks with nothing to report are marked done without a message.
func (a *App) sendDigests(ctx context.Context) (int, error) {
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// A time zone PostgreSQL doesn't know (saved before it was checked, or one only Go knows)
	// counts as UTC: AT TIME ZONE would otherwise fail the query for everyone.
	rows, err := tx.Query(ctx, `WITH local AS (
			SELECT u.id, now() AT TIME ZONE coalesce(tz.name, 'UTC') AS now FROM users u
			LEFT JOIN (SELECT name FROM pg_timezone_names) tz ON tz.name = u.timezone
			WHERE u.status = 'active'),
		due AS (
			INSERT INTO digests_sent (user_id, week)
			SELECT id, date_trunc('week', local.now)::date FROM local
			WHERE local.now >= date_trunc('week', local.now) + interval '9 hours'
			ON CONFLICT DO NOTHING
			RETURNING user_id),
		docs AS (
			SELECT due.user_id, d.added_at, d.inbox, d.status
			FROM due JOIN space_members m ON m.user_id = due.user_id
			JOIN documents d ON d.space_id = m.space_id AND d.deleted_at IS NULL)
		SELECT due.user_id,
			(SELECT count(*) FROM docs WHERE docs.user_id = due.user_id AND added_at > now() - interval '7 days'),
			(SELECT count(*) FROM docs WHERE docs.user_id = due.user_id AND inbox),
			(SELECT count(*) FROM docs WHERE docs.user_id = due.user_id AND status IN ('failed', 'needs_password')),
			(SELECT count(*) FROM space_members m
				JOIN documents d ON d.space_id = m.space_id AND d.deleted_at IS NULL
				JOIN custom_field_values v ON v.document_id = d.id
				JOIN custom_fields f ON f.id = v.field_id AND f.remind
				WHERE m.user_id = due.user_id AND v.value_date BETWEEN current_date AND current_date + 14)
		FROM due`)
	if err != nil {
		return 0, err
	}
	type digest struct {
		user                           uuid.UUID
		added, inbox, failed, upcoming int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (digest, error) {
		var d digest
		return d, r.Scan(&d.user, &d.added, &d.inbox, &d.failed, &d.upcoming)
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range list {
		body := digestBody(d.added, d.inbox, d.failed, d.upcoming)
		if body == "" {
			continue
		}
		if err := a.Queue.EmitTx(ctx, tx, jobs.Event{Type: "digest.weekly", Title: "Your week in Docveta", Body: body,
			Link: "/", Recipients: []uuid.UUID{d.user}}); err != nil {
			return 0, err
		}
		sent++
	}
	return sent, tx.Commit(ctx)
}

// digestBody is the summary's text, or "" when there is nothing to report.
func digestBody(added, inbox, failed, upcoming int) string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return thousands(n) + " " + many
	}
	var parts []string
	if added > 0 {
		parts = append(parts, plural(added, "document", "documents")+" added")
	}
	if inbox > 0 {
		parts = append(parts, plural(inbox, "waits", "wait")+" for review in the Inbox")
	}
	if failed > 0 {
		parts = append(parts, plural(failed, "needs", "need")+" attention")
	}
	if upcoming > 0 {
		parts = append(parts, plural(upcoming, "date", "dates")+" coming up in the next two weeks")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + "."
}

// thousands writes a count as people read it: 12,480 rather than 12480.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
