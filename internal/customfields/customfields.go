// Package customfields implements per-space custom fields (Amount, Due date, Policy
// number, ...): their definitions, typed values on documents, and validation.
package customfields

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
)

// Data types of a field. Values are stored in typed columns so they can be filtered and
// sorted with indexes (amounts as numbers, dates as dates).
const (
	Text        = "text"
	LongText    = "longtext"
	Integer     = "integer"
	Decimal     = "decimal"
	Monetary    = "monetary"
	Date        = "date"
	Boolean     = "boolean"
	URL         = "url"
	Select      = "select"
	MultiSelect = "multiselect"
	Document    = "document"
)

var dataTypes = []string{Text, LongText, Integer, Decimal, Monetary, Date, Boolean, URL, Select, MultiSelect, Document}

// Field is a custom field definition.
type Field struct {
	ID            uuid.UUID      `json:"id"`
	SpaceID       uuid.UUID      `json:"space_id"`
	Name          string         `json:"name"`
	DataType      string         `json:"data_type"`
	Options       map[string]any `json:"options"` // select: {"choices":[...]}; monetary: {"currency":"INR"}
	DocumentCount int            `json:"document_count"`
}

type Input struct {
	Name     *string         `json:"name"`
	DataType *string         `json:"data_type"`
	Options  json.RawMessage `json:"options"`
}

// Value is one field's value on a document. Value is a string, number, bool, []string or
// null, depending on the field type.
type Value struct {
	FieldID  uuid.UUID `json:"field_id"`
	Name     string    `json:"name"`
	DataType string    `json:"data_type"`
	Value    any       `json:"value"`
	Currency string    `json:"currency,omitempty"`
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
	// Reindex re-indexes documents' metadata after values were removed in bulk.
	Reindex func(ctx context.Context, ids []uuid.UUID) error
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service) *Service {
	return &Service{pool: pool, spaces: sp}
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

// normalize validates input and returns the cleaned name, type and options.
func normalize(in *Input, create bool, oldType string) (name, typ string, options []byte, err error) {
	var v apperr.Validation
	if in.Name != nil {
		name = strings.Join(strings.Fields(*in.Name), " ")
		if name == "" || utf8.RuneCountInString(name) > 60 {
			v.Add("name", "Name must be 1–60 characters")
		}
		// Names are used in search as cf:Name>5, so keep them free of operator characters.
		if strings.ContainsAny(name, "<>=!~\"") {
			v.Add("name", "Name can't contain < > = ! ~ or quotes")
		}
	} else if create {
		v.Add("name", "Name is required")
	}
	typ = oldType
	if in.DataType != nil {
		typ = *in.DataType
		if !slices.Contains(dataTypes, typ) {
			v.Add("data_type", "Unknown field type")
		} else if oldType != "" && oldType != typ {
			v.Add("data_type", "A field's type can't be changed after it's created")
		}
	} else if create {
		v.Add("data_type", "Type is required")
	}
	opts := map[string]any{}
	if len(in.Options) > 0 {
		if err := json.Unmarshal(in.Options, &opts); err != nil {
			v.Add("options", "Invalid options")
		}
	}
	switch typ {
	case Select, MultiSelect:
		var choices []string
		if raw, ok := opts["choices"].([]any); ok {
			for _, c := range raw {
				if s, _ := c.(string); strings.TrimSpace(s) != "" && !slices.Contains(choices, strings.TrimSpace(s)) {
					choices = append(choices, strings.TrimSpace(s))
				}
			}
		}
		if len(choices) == 0 && (create || in.Options != nil) {
			v.Add("options", "Add at least one choice")
		}
		if len(choices) > 100 {
			v.Add("options", "At most 100 choices")
		}
		opts = map[string]any{"choices": choices}
	case Monetary:
		cur, _ := opts["currency"].(string)
		cur = strings.ToUpper(strings.TrimSpace(cur))
		if cur == "" {
			cur = "INR"
		}
		if !currencyRe.MatchString(cur) {
			v.Add("options", "Use a 3-letter currency code like INR or USD")
		}
		opts = map[string]any{"currency": cur}
	default:
		opts = map[string]any{}
	}
	if err := v.Err(); err != nil {
		return "", "", nil, err
	}
	options, _ = json.Marshal(opts)
	return name, typ, options, nil
}

func scanField(row pgx.Row) (*Field, error) {
	var f Field
	var opts []byte
	err := row.Scan(&f.ID, &f.SpaceID, &f.Name, &f.DataType, &opts, &f.DocumentCount)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Custom field")
	}
	if err != nil {
		return nil, err
	}
	f.Options = map[string]any{}
	_ = json.Unmarshal(opts, &f.Options)
	return &f, nil
}

const selectSQL = `SELECT f.id, f.space_id, f.name, f.data_type, f.options,
	(SELECT count(*) FROM custom_field_values v JOIN documents d ON d.id=v.document_id WHERE v.field_id=f.id AND d.deleted_at IS NULL)
	FROM custom_fields f`

// List returns the custom fields of one space, or of all spaces the caller can see.
func (s *Service) List(ctx context.Context, p *auth.Principal, spaceID *uuid.UUID) ([]*Field, error) {
	var ids []uuid.UUID
	if spaceID != nil {
		if _, err := s.spaces.Require(ctx, p, *spaceID, spaces.ActView); err != nil {
			return nil, err
		}
		ids = []uuid.UUID{*spaceID}
	} else {
		var err error
		if ids, err = s.spaces.VisibleSpaceIDs(ctx, p.UserID); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, selectSQL+` WHERE f.space_id = ANY($1) ORDER BY lower(f.name)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Field{}
	for rows.Next() {
		f, err := scanField(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Field, error) {
	return scanField(s.pool.QueryRow(ctx, selectSQL+` WHERE f.id=$1`, id))
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, spaceID uuid.UUID, in Input) (*Field, error) {
	if _, err := s.spaces.Require(ctx, p, spaceID, spaces.ActEdit); err != nil {
		return nil, err
	}
	name, typ, opts, err := normalize(&in, true, "")
	if err != nil {
		return nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO custom_fields (id, space_id, name, data_type, options) VALUES ($1,$2,$3,$4,$5)`,
		id, spaceID, name, typ, opts); err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("name_taken", "A field called \""+name+"\" already exists in this space")
		}
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*Field, error) {
	f, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.spaces.Require(ctx, p, f.SpaceID, spaces.ActEdit); err != nil {
		return nil, apperr.NotFound("Custom field")
	}
	if in.Name == nil && in.Options == nil && in.DataType == nil {
		return f, nil
	}
	name, _, opts, err := normalize(&in, false, f.DataType)
	if err != nil {
		return nil, err
	}
	if in.Name == nil {
		name = f.Name
	}
	if in.Options == nil {
		opts, _ = json.Marshal(f.Options)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE custom_fields SET name=$2, options=$3 WHERE id=$1`, id, name, opts); err != nil {
		if db.IsUniqueViolation(err) {
			return nil, apperr.Conflict("name_taken", "A field called \""+name+"\" already exists in this space")
		}
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	f, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.spaces.Require(ctx, p, f.SpaceID, spaces.ActEdit); err != nil {
		return apperr.NotFound("Custom field")
	}
	rows, err := s.pool.Query(ctx, `SELECT document_id FROM custom_field_values WHERE field_id=$1`, id)
	if err != nil {
		return err
	}
	docs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM custom_fields WHERE id=$1`, id); err != nil {
		return err
	}
	if s.Reindex != nil && len(docs) > 0 {
		return s.Reindex(ctx, docs)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

type stored struct {
	text *string
	num  *float64
	date *string
	b    *bool
	js   []byte
}

// parseValue validates raw against the field and returns the columns to store.
// A nil result means "clear the value".
func parseValue(f *Field, raw json.RawMessage) (*stored, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	bad := func(msg string, args ...any) error { return fmt.Errorf(msg, args...) }
	var st stored
	switch f.DataType {
	case Text, LongText, URL, Select, Document:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, bad("Enter text")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, nil
		}
		limit := 500
		if f.DataType == LongText {
			limit = 20000
		}
		if utf8.RuneCountInString(s) > limit {
			return nil, bad("That's too long")
		}
		switch f.DataType {
		case URL:
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, bad("Enter a web address starting with http:// or https://")
			}
		case Select:
			choices, _ := f.Options["choices"].([]any)
			ok := false
			for _, c := range choices {
				ok = ok || c == s
			}
			if !ok {
				return nil, bad("Choose one of the listed options")
			}
		case Document:
			if _, err := uuid.Parse(s); err != nil {
				return nil, bad("Choose a document")
			}
		}
		st.text = &s
	case Integer, Decimal, Monetary:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil {
			// Accept numbers typed as text, with thousands separators ("1,24,500.50").
			var s string
			if json.Unmarshal(raw, &s) != nil {
				return nil, bad("Enter a number")
			}
			s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), " ", ""))
			if s == "" {
				return nil, nil
			}
			var perr error
			if n, perr = strconv.ParseFloat(s, 64); perr != nil {
				return nil, bad("Enter a number")
			}
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e15 {
			return nil, bad("That number is out of range")
		}
		if f.DataType == Integer && n != math.Trunc(n) {
			return nil, bad("Enter a whole number")
		}
		st.num = &n
	case Date:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, bad("Enter a date")
		}
		if s = strings.TrimSpace(s); s == "" {
			return nil, nil
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, bad("Use the format YYYY-MM-DD")
		}
		st.date = &s
	case Boolean:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, bad("Choose yes or no")
		}
		st.b = &b
	case MultiSelect:
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, bad("Choose from the listed options")
		}
		choices, _ := f.Options["choices"].([]any)
		var out []string
		for _, s := range list {
			ok := false
			for _, c := range choices {
				ok = ok || c == s
			}
			if !ok {
				return nil, bad("%q isn't one of the options", s)
			}
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		st.js, _ = json.Marshal(out)
	default:
		return nil, bad("Unsupported field type")
	}
	return &st, nil
}

// SetValues writes values (keyed by field id; null clears) for a document inside tx.
// Fields must belong to the document's space. It returns what changed, for history.
func SetValues(ctx context.Context, q db.Querier, docID, spaceID uuid.UUID, in map[string]json.RawMessage, source string) (map[string]any, error) {
	if len(in) == 0 {
		return nil, nil
	}
	var v apperr.Validation
	changes := map[string]any{}
	for key, raw := range in {
		fid, err := uuid.Parse(key)
		if err != nil {
			v.Add("custom_fields."+key, "Unknown field")
			continue
		}
		var f Field
		var opts []byte
		err = q.QueryRow(ctx, `SELECT id, space_id, name, data_type, options FROM custom_fields WHERE id=$1`, fid).
			Scan(&f.ID, &f.SpaceID, &f.Name, &f.DataType, &opts)
		if err != nil || f.SpaceID != spaceID {
			v.Add("custom_fields."+key, "This field doesn't exist in the document's space")
			continue
		}
		_ = json.Unmarshal(opts, &f.Options)
		st, perr := parseValue(&f, raw)
		if perr != nil {
			v.Add("custom_fields."+key, "%s: %s", f.Name, perr.Error())
			continue
		}
		if f.DataType == Document && st != nil {
			// The linked document must exist.
			var ok bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE id=$1::uuid)`, *st.text).Scan(&ok); err != nil || !ok {
				v.Add("custom_fields."+key, "%s: that document doesn't exist", f.Name)
				continue
			}
		}
		if st == nil {
			if _, err := q.Exec(ctx, `DELETE FROM custom_field_values WHERE document_id=$1 AND field_id=$2`, docID, fid); err != nil {
				return nil, err
			}
			changes[f.Name] = nil
			continue
		}
		if _, err := q.Exec(ctx, `INSERT INTO custom_field_values (document_id, field_id, value_text, value_number, value_date, value_bool, value_json, source)
			VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8)
			ON CONFLICT (document_id, field_id) DO UPDATE SET value_text=excluded.value_text, value_number=excluded.value_number,
				value_date=excluded.value_date, value_bool=excluded.value_bool, value_json=excluded.value_json, source=excluded.source`,
			docID, fid, st.text, st.num, st.date, st.b, st.js, source); err != nil {
			return nil, err
		}
		changes[f.Name] = json.RawMessage(raw)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

// Load returns the values of the given documents, grouped by document, ordered by field name.
func Load(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID][]Value, error) {
	out := map[uuid.UUID][]Value{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT v.document_id, f.id, f.name, f.data_type, f.options, v.value_text, v.value_number::float8,
			to_char(v.value_date, 'YYYY-MM-DD'), v.value_bool, v.value_json
		FROM custom_field_values v JOIN custom_fields f ON f.id=v.field_id
		WHERE v.document_id = ANY($1) ORDER BY lower(f.name)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var docID uuid.UUID
		var val Value
		var opts, js []byte
		var text, date *string
		var num *float64
		var b *bool
		if err := rows.Scan(&docID, &val.FieldID, &val.Name, &val.DataType, &opts, &text, &num, &date, &b, &js); err != nil {
			return nil, err
		}
		switch val.DataType {
		case Integer:
			if num != nil {
				val.Value = int64(*num)
			}
		case Decimal, Monetary:
			if num != nil {
				val.Value = *num
			}
		case Date:
			if date != nil {
				val.Value = *date
			}
		case Boolean:
			if b != nil {
				val.Value = *b
			}
		case MultiSelect:
			var list []string
			_ = json.Unmarshal(js, &list)
			val.Value = list
		default:
			if text != nil {
				val.Value = *text
			}
		}
		if val.DataType == Monetary {
			var o map[string]any
			_ = json.Unmarshal(opts, &o)
			val.Currency, _ = o["currency"].(string)
		}
		out[docID] = append(out[docID], val)
	}
	return out, rows.Err()
}

// RemapOnMove keeps a moved document's values where the target space has a field of the
// same name and type, and drops the rest.
func RemapOnMove(ctx context.Context, q db.Querier, docID, to uuid.UUID) error {
	if _, err := q.Exec(ctx, `UPDATE custom_field_values v SET field_id = t.id
		FROM custom_fields f JOIN custom_fields t ON lower(t.name)=lower(f.name) AND t.data_type=f.data_type AND t.space_id=$2
		WHERE v.document_id=$1 AND v.field_id=f.id AND f.space_id<>$2`, docID, to); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `DELETE FROM custom_field_values v USING custom_fields f
		WHERE v.document_id=$1 AND v.field_id=f.id AND f.space_id<>$2`, docID, to)
	return err
}
