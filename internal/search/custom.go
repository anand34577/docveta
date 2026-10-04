package search

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
)

// fieldNames lists the (lower-cased) custom field names of the given spaces, so
// "amount:>1500" can be told apart from ordinary words that contain a colon.
func (s *Service) fieldNames(ctx context.Context, spaceIDs []uuid.UUID) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT lower(name) FROM custom_fields WHERE space_id = ANY($1)`, spaceIDs)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "yes", "y", "1":
		return true, true
	case "false", "no", "n", "0":
		return false, true
	}
	return false, false
}

var cmpOps = map[string]string{"=": "=", "!=": "<>", ">": ">", ">=": ">=", "<": "<", "<=": "<="}

// customCond builds the condition for one custom field comparison. A field of the
// document's own space is matched by name; the value is read as a number, a date, a
// yes/no or text, whichever the field's type can compare.
func customCond(b *builder, f CustomFilter) string {
	base := "EXISTS (SELECT 1 FROM custom_field_values v JOIN custom_fields cf ON cf.id=v.field_id" +
		" WHERE v.document_id=d.id AND cf.space_id=d.space_id AND lower(cf.name)=" + b.arg(strings.ToLower(f.Name))
	val := strings.TrimSpace(f.Value)
	if f.Op == "" || (val == "" && f.Op == "=") {
		return base + ")"
	}
	op, cmp := f.Op, cmpOps[f.Op]
	var alts []string
	if n, err := strconv.ParseFloat(strings.ReplaceAll(val, ",", ""), 64); err == nil && cmp != "" {
		alts = append(alts, "(cf.data_type IN ('integer','decimal','monetary') AND v.value_number "+cmp+" "+b.arg(n)+")")
	}
	if cmp != "" {
		if from, to := dateRange(val); from != nil && to != nil {
			col := "v.value_date"
			switch op {
			case "=":
				alts = append(alts, "(cf.data_type='date' AND "+col+" BETWEEN "+b.arg(*from)+"::date AND "+b.arg(*to)+"::date)")
			case "!=":
				alts = append(alts, "(cf.data_type='date' AND "+col+" NOT BETWEEN "+b.arg(*from)+"::date AND "+b.arg(*to)+"::date)")
			case ">", "<=":
				alts = append(alts, "(cf.data_type='date' AND "+col+" "+cmp+" "+b.arg(*to)+"::date)")
			default: // >=, <
				alts = append(alts, "(cf.data_type='date' AND "+col+" "+cmp+" "+b.arg(*from)+"::date)")
			}
		}
	}
	if bv, ok := parseBool(val); ok && (op == "=" || op == "!=") {
		alts = append(alts, "(cf.data_type='boolean' AND v.value_bool "+cmp+" "+b.arg(bv)+")")
	}
	if val != "" && (op == "=" || op == "!=" || op == "~") {
		t := b.arg(val) + "::text" // allocated only when used: PostgreSQL rejects unused parameters
		textTypes := "cf.data_type IN ('text','longtext','url','select','document')"
		elem := "EXISTS (SELECT 1 FROM jsonb_array_elements_text(v.value_json) e WHERE %s)"
		switch op {
		case "=":
			alts = append(alts, "("+textTypes+" AND lower(v.value_text)=lower("+t+"))",
				"(cf.data_type='multiselect' AND "+strings.Replace(elem, "%s", "lower(e)=lower("+t+")", 1)+")")
		case "!=":
			alts = append(alts, "("+textTypes+" AND lower(v.value_text)<>lower("+t+"))",
				"(cf.data_type='multiselect' AND NOT "+strings.Replace(elem, "%s", "lower(e)=lower("+t+")", 1)+")")
		case "~":
			alts = append(alts, "("+textTypes+" AND strpos(lower(v.value_text), lower("+t+")) > 0)",
				"(cf.data_type='multiselect' AND "+strings.Replace(elem, "%s", "strpos(lower(e), lower("+t+")) > 0", 1)+")")
		}
	}
	if len(alts) == 0 {
		return "false" // the value can't be compared with any field type
	}
	return base + " AND (" + strings.Join(alts, " OR ") + "))"
}

// customOrder returns the ORDER BY expression for sorting by a custom field (by id).
func (s *Service) customOrder(ctx context.Context, fieldID string, spaceIDs []uuid.UUID) (string, error) {
	id, err := uuid.Parse(fieldID)
	if err != nil {
		return "", apperr.Invalid("sort", "Unknown sort order")
	}
	var typ string
	if err := s.pool.QueryRow(ctx, `SELECT data_type FROM custom_fields WHERE id=$1 AND space_id = ANY($2)`, id, spaceIDs).Scan(&typ); err != nil {
		return "", apperr.Invalid("sort", "Unknown custom field")
	}
	col := "v.value_text"
	switch typ {
	case "integer", "decimal", "monetary":
		col = "v.value_number"
	case "date":
		col = "v.value_date"
	case "boolean":
		col = "v.value_bool"
	case "multiselect":
		col = "v.value_json::text"
	default:
		col = "lower(v.value_text)"
	}
	// The id was parsed as a UUID, so it is safe to inline (and keeps the count query's arguments unchanged).
	return "(SELECT " + col + " FROM custom_field_values v WHERE v.document_id=d.id AND v.field_id='" + id.String() + "'::uuid)", nil
}

// MatchAll returns up to limit documents of a space that satisfy a query, newest first
// (used by scheduled workflows).
func (s *Service) MatchAll(ctx context.Context, spaceID uuid.UUID, q Query, limit int) ([]uuid.UUID, error) {
	q.SpaceIDs, q.IDs, q.WithTotal, q.Trash, q.Sort, q.Mode, q.Cursor, q.Limit = []uuid.UUID{spaceID}, nil, false, false, "-added", "", "", 200
	var out []uuid.UUID
	for len(out) < limit {
		res, err := s.run(ctx, []uuid.UUID{spaceID}, q)
		if err != nil {
			return nil, err
		}
		for _, h := range res.Hits {
			out = append(out, h.ID)
		}
		if res.NextCursor == nil || len(res.Hits) == 0 {
			break
		}
		q.Cursor = *res.NextCursor
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
