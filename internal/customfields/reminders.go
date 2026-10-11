package customfields

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/jobs"
)

// Upcoming is a date set to remind, on one document.
type Upcoming struct {
	DocumentID uuid.UUID `json:"document_id"`
	Title      string    `json:"title"`
	SpaceID    uuid.UUID `json:"space_id"`
	FieldID    uuid.UUID `json:"field_id"`
	FieldName  string    `json:"field_name"`
	Due        string    `json:"due"`       // YYYY-MM-DD
	DaysLeft   int       `json:"days_left"` // negative once the date has passed
}

// Upcoming lists the dates set to remind that fall within the next `days` days, plus the
// ones passed in the last 30 (an expired passport still needs doing), soonest first.
func (s *Service) Upcoming(ctx context.Context, p *auth.Principal, days int) ([]Upcoming, error) {
	ids, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id, d.title, d.space_id, f.id, f.name, to_char(v.value_date, 'YYYY-MM-DD'), v.value_date - current_date
		FROM custom_field_values v
		JOIN custom_fields f ON f.id = v.field_id AND f.remind
		JOIN documents d ON d.id = v.document_id AND d.deleted_at IS NULL
		WHERE d.space_id = ANY($1) AND v.value_date BETWEEN current_date - 30 AND current_date + $2::int
		ORDER BY v.value_date, lower(d.title) LIMIT 100`, ids, days)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Upcoming, error) {
		var u Upcoming
		err := r.Scan(&u.DocumentID, &u.Title, &u.SpaceID, &u.FieldID, &u.FieldName, &u.Due, &u.DaysLeft)
		return u, err
	})
}

// SendDue notifies the members of a document's space when one of its dates set to remind is
// within one of the field's lead times. Each (date, lead time) goes out once; after a pause
// (server off for a week) only the nearest lead time is sent, not every one missed.
// ponytail: "today" is the database's date, per-user time zones if anyone needs reminders at their midnight.
func (s *Service) SendDue(ctx context.Context, q *jobs.Queue) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH due AS (
			SELECT v.document_id, f.id AS field_id, v.value_date AS due,
				(SELECT min(l) FROM unnest(f.remind_lead_days) l WHERE l >= v.value_date - current_date) AS lead
			FROM custom_field_values v
			JOIN custom_fields f ON f.id = v.field_id AND f.remind
			JOIN documents d ON d.id = v.document_id AND d.deleted_at IS NULL
			WHERE v.value_date BETWEEN current_date AND current_date + 366)
		INSERT INTO reminders_sent (document_id, field_id, due, lead_days)
		SELECT document_id, field_id, due, lead FROM due WHERE lead IS NOT NULL
		ON CONFLICT DO NOTHING
		RETURNING document_id, field_id`)
	if err != nil {
		return 0, err
	}
	type key struct{ doc, field uuid.UUID }
	sent, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (key, error) {
		var k key
		return k, r.Scan(&k.doc, &k.field)
	})
	if err != nil {
		return 0, err
	}
	for _, k := range sent {
		var title, field string
		var space uuid.UUID
		var due time.Time
		var left int
		var members []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT d.title, f.name, d.space_id, v.value_date, v.value_date - current_date,
				ARRAY(SELECT user_id FROM space_members m WHERE m.space_id = d.space_id)
			FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id JOIN documents d ON d.id = v.document_id
			WHERE v.document_id=$1 AND v.field_id=$2`, k.doc, k.field).Scan(&title, &field, &space, &due, &left, &members); err != nil {
			return 0, err
		}
		doc := k.doc
		if err := q.EmitTx(ctx, tx, jobs.Event{Type: "reminder.due", Title: fmt.Sprintf("%s: %s %s", title, field, whenText(left)),
			Body: fmt.Sprintf("%s is on %s.", field, due.Format("2 Jan 2006")), Link: "/documents/" + doc.String(),
			Severity: severity(left), Recipients: members, SpaceID: &space, DocumentID: &doc}); err != nil {
			return 0, err
		}
	}
	return len(sent), tx.Commit(ctx)
}

func whenText(days int) string {
	switch days {
	case 0:
		return "is today"
	case 1:
		return "is tomorrow"
	default:
		return fmt.Sprintf("in %d days", days)
	}
}

func severity(days int) string {
	if days <= 7 {
		return "warning"
	}
	return "info"
}
