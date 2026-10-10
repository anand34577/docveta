package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/taxonomy"
)

// How sure the AI must be before a space in "apply automatically" mode applies a
// suggestion, and before it may propose a tag, correspondent or document type that doesn't
// exist yet, and how many tags it may invent for one document (so a talkative model doesn't
// fill a space with tags nobody asked for), are each space's own settings: see
// spaces.Space.AIAutoConfidence and its neighbours.

// commonTypes are the default for Tuning.CommonTypes: the broad kinds of documents offered to the model next to the space's own
// document types, so that spaces which haven't set any up still get the same few names
// instead of a new spelling for every document. The type is the drawer a document goes in
// (Identification); what exactly it is (Aadhaar, PAN) is left to the tags.
var commonTypes = []string{"Identification", "Banking", "Tax", "Bill", "Invoice", "Receipt", "Insurance", "Medical",
	"Education", "Employment", "Property", "Vehicle", "Legal", "Contract", "Certificate", "Travel", "Warranty",
	"Letter", "Report", "Manual"}

// Suggestion is one proposed change to a document.
type Suggestion struct {
	ID         uuid.UUID       `json:"id"`
	Field      string          `json:"field"`
	Value      json.RawMessage `json:"value"`
	Confidence float32         `json:"confidence"`
	Status     string          `json:"status"`
}

type suggValue struct {
	Name    string `json:"name"`
	ID      string `json:"id,omitempty"`       // existing tag/correspondent/type
	New     bool   `json:"new,omitempty"`      // a tag/correspondent/type that doesn't exist yet
	FieldID string `json:"field_id,omitempty"` // custom field
	Field   string `json:"field,omitempty"`    // custom field name, for display
}

var classifySchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"title", "document_date", "correspondent", "document_type", "tags", "custom_fields"},
	"properties": map[string]any{
		"title":         map[string]any{"type": []string{"string", "null"}},
		"document_date": map[string]any{"type": []string{"string", "null"}},
		"correspondent": nameConf(),
		"document_type": nameConf(),
		"tags": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false,
			"required": []string{"name", "confidence"}, "properties": map[string]any{"name": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number"}}}},
		"custom_fields": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false,
			"required": []string{"name", "value", "confidence"}, "properties": map[string]any{"name": map[string]any{"type": "string"},
				"value": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number"}}}},
	},
}

func nameConf() map[string]any {
	return map[string]any{"anyOf": []any{
		map[string]any{"type": "null"},
		map[string]any{"type": "object", "additionalProperties": false, "required": []string{"name", "confidence"},
			"properties": map[string]any{"name": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number"}}},
	}}
}

type nameConfidence struct {
	Name       string  `json:"name"`
	Confidence float32 `json:"confidence"`
}

type classifyResult struct {
	Title         *string          `json:"title"`
	DocumentDate  *string          `json:"document_date"`
	Correspondent *nameConfidence  `json:"correspondent"`
	DocumentType  *nameConfidence  `json:"document_type"`
	Tags          []nameConfidence `json:"tags"`
	CustomFields  []struct {
		Name       string  `json:"name"`
		Value      string  `json:"value"`
		Confidence float32 `json:"confidence"`
	} `json:"custom_fields"`
}

type vocab struct {
	id   uuid.UUID
	name string
}

func (s *Service) vocabulary(ctx context.Context, table string, spaceID uuid.UUID, count string, limit int) ([]vocab, error) {
	// Most used first, capped, so a huge vocabulary doesn't swamp the prompt.
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.name FROM `+table+` t WHERE t.space_id=$1 ORDER BY `+count+` DESC, lower(t.name) LIMIT $2`, spaceID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (vocab, error) {
		var v vocab
		err := r.Scan(&v.id, &v.name)
		return v, err
	})
}

func names(v []vocab) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = x.name
	}
	return out
}

func find(v []vocab, name string) *vocab {
	for i := range v {
		if strings.EqualFold(v[i].name, strings.TrimSpace(name)) {
			return &v[i]
		}
	}
	return nil
}

// cleanName tidies a name the model made up; "" when it isn't usable as one.
func cleanName(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || utf8.RuneCountInString(s) > maxRunes {
		return ""
	}
	return s
}

// canonical returns the list's own spelling of name, or name when it isn't in the list.
func canonical(list []string, name string) string {
	for _, x := range list {
		if strings.EqualFold(x, name) {
			return x
		}
	}
	return name
}

// ClassifyDocument asks the AI to organise a document and records its answer as
// suggestions (or applies the confident ones, in "apply automatically" spaces). It never
// returns an error that should hold up processing: callers log and carry on.
func (s *Service) ClassifyDocument(ctx context.Context, docID uuid.UUID) error {
	var spaceID uuid.UUID
	var title, content, dateFormat, applyMode string
	var corr, typ *uuid.UUID
	var docDate *time.Time
	var deleted, newTags, newTypes bool
	var autoPct, newPct, maxNewTags int
	err := s.pool.QueryRow(ctx, `SELECT d.space_id, d.title, d.content, d.correspondent_id, d.document_type_id, d.document_date, d.deleted_at IS NOT NULL,
		coalesce(u.date_format,'DD/MM/YYYY'), sp.ai_apply_mode, sp.ai_new_tags,
		sp.ai_new_types, sp.ai_auto_confidence, sp.ai_new_confidence, sp.ai_max_new_tags
		FROM documents d JOIN spaces sp ON sp.id=d.space_id LEFT JOIN users u ON u.id=d.owner_id WHERE d.id=$1`, docID).
		Scan(&spaceID, &title, &content, &corr, &typ, &docDate, &deleted, &dateFormat, &applyMode, &newTags, &newTypes, &autoPct, &newPct, &maxNewTags)
	if db.IsNoRows(err) || deleted {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil // nothing to read
	}
	prov, client, err := s.forSpace(ctx, spaceID, false)
	if err != nil {
		return nil // off, not set up or not allowed: the document just isn't AI-classified
	}

	tune := s.tuning(ctx)
	autoApplyConfidence, newNameConfidence := float32(autoPct)/100, float32(newPct)/100
	// A small context window (local models often run with 4096 tokens) gets shorter lists and
	// fewer examples, so that most of it is left for the document itself.
	window := prov.contextTokens()
	listLimit, exampleLimit := 150, 5
	if window < defaultContextTokens {
		listLimit, exampleLimit = 40, 2
	}
	tags, err := s.vocabulary(ctx, "tags", spaceID, `(SELECT count(*) FROM document_tags dt WHERE dt.tag_id=t.id)`, listLimit)
	if err != nil {
		return err
	}
	corrs, err := s.vocabulary(ctx, "correspondents", spaceID, `(SELECT count(*) FROM documents d WHERE d.correspondent_id=t.id)`, listLimit)
	if err != nil {
		return err
	}
	types, err := s.vocabulary(ctx, "document_types", spaceID, `(SELECT count(*) FROM documents d WHERE d.document_type_id=t.id)`, listLimit)
	if err != nil {
		return err
	}
	userSet := map[string]bool{}
	rows, err := s.pool.Query(ctx, `SELECT field FROM field_sources WHERE document_id=$1 AND source='user'`, docID)
	if err != nil {
		return err
	}
	fs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, f := range fs {
		userSet[f] = true
	}
	fields, err := s.fieldsOf(ctx, spaceID)
	if err != nil {
		return err
	}
	examples := s.examples(ctx, spaceID, docID, exampleLimit)

	dayFirst := !strings.HasPrefix(dateFormat, "MM")
	order := "day first (03/04/2026 is 3 April 2026)"
	if !dayFirst {
		order = "month first (03/04/2026 is April 3, 2026)"
	}
	var fieldNames []string
	for _, f := range fields {
		fieldNames = append(fieldNames, fmt.Sprintf("%s (%s)", f.Name, f.DataType))
	}
	tagRule := `- tags say what exactly the document is and what it is about. Pick them ONLY from the existing tags.`
	if newTags {
		tagRule = `- tags say what exactly the document is and what it is about, five at most (for an identity card: "Aadhaar" or "PAN"; for a bill: "Electricity"). Use the existing tags when they fit. If something important has no tag yet, you may add a new one: one to three words that other documents of the same kind will share, never a person's name, a number or a date. Don't repeat the document type as a tag.`
	}
	typeRule := `- document_type is the broad kind of document, the drawer it would be filed in. Pick it ONLY from the known document types; use null when none fits.`
	if newTypes {
		typeRule = `- document_type is the broad kind of document, the drawer it would be filed in (an Aadhaar or PAN card is "Identification", a bank statement is "Banking"). Pick it from the known document types when one fits, otherwise from the common document types; only when neither has it, give a short new name. Use null if you can't tell.`
	}
	sys := `You help a person organise their scanned documents. Read the document and propose how to file it.
Rules:
` + tagRule + `
` + typeRule + `
- correspondent is who sent or issued the document. Pick it from the known correspondents when one fits; if none fits, you may give its name as printed, otherwise use null.
- document_date is the date printed on the document (issue/statement date), as YYYY-MM-DD, or null if unsure. Dates in this person's documents are written ` + order + `.
- title is a short, clear title if the current one is poor, otherwise null.
- custom_fields: only for the listed fields, value as plain text (numbers without currency symbols, dates as YYYY-MM-DD).
- confidence is between 0 and 1. Don't guess: lower confidence or null is better than a wrong answer.
- Reply with JSON only.`
	var user strings.Builder
	fmt.Fprintf(&user, "Current title: %s\n", title)
	fmt.Fprintf(&user, "Existing tags: %s\n", jsonList(names(tags)))
	fmt.Fprintf(&user, "Known correspondents: %s\n", jsonList(names(corrs)))
	fmt.Fprintf(&user, "Known document types: %s\n", jsonList(names(types)))
	if newTypes {
		fmt.Fprintf(&user, "Common document types: %s\n", jsonList(tune.CommonTypes))
	}
	if len(fieldNames) > 0 {
		fmt.Fprintf(&user, "Custom fields: %s\n", jsonList(fieldNames))
	}
	if examples != "" {
		fmt.Fprintf(&user, "\nHow this person has filed similar documents:\n%s\n", examples)
	}
	// What is left of the window after the instructions, the lists and room for the answer
	// goes to the document's text.
	room := window - classifyReplyTokens - estTokens(sys) - estTokens(user.String())
	fmt.Fprintf(&user, "\n--- DOCUMENT TEXT ---\n%s", excerpt(content, min(max(room, 300), tune.SuggestTextTokens)))

	cctx, cancel := context.WithTimeout(ctx, time.Duration(max(prov.TimeoutSeconds, 30))*time.Second)
	defer cancel()
	raw, err := client.ChatJSON(cctx, []Message{{Role: "system", Content: sys}, {Role: "user", Content: user.String()}}, classifySchema)
	s.markResult(ctx, prov.ID, err)
	if err != nil {
		slog.Warn("AI classification failed", "document", docID, "err", err)
		return nil
	}
	var res classifyResult
	if err := json.Unmarshal(raw, &res); err != nil {
		slog.Warn("AI classification: unreadable answer", "document", docID, "err", err)
		return nil
	}

	var sugs []Suggestion
	add := func(field string, v suggValue, conf float32) {
		b, _ := json.Marshal(v)
		sugs = append(sugs, Suggestion{ID: uuid.Must(uuid.NewV7()), Field: field, Value: b, Confidence: clamp(conf), Status: "pending"})
	}
	have := map[uuid.UUID]bool{}
	hrows, err := s.pool.Query(ctx, `SELECT tag_id FROM document_tags WHERE document_id=$1`, docID)
	if err != nil {
		return err
	}
	hv, err := pgx.CollectRows(hrows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, t := range hv {
		have[t] = true
	}
	seen := map[string]bool{}
	invented := 0
	for _, t := range res.Tags {
		name := cleanName(t.Name, 40)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		v := find(tags, name)
		if v == nil {
			// Not among the most used tags shown to the model, but it may exist all the same.
			var x vocab
			err := s.pool.QueryRow(ctx, `SELECT id, name FROM tags WHERE space_id=$1 AND lower(name)=lower($2)`, spaceID, name).Scan(&x.id, &x.name)
			if err == nil {
				v = &x
			} else if !db.IsNoRows(err) {
				return err
			}
		}
		switch {
		case v != nil && !have[v.id]:
			add("tag", suggValue{Name: v.name, ID: v.id.String()}, t.Confidence)
		case v == nil && newTags && t.Confidence >= newNameConfidence && invented < maxNewTags:
			invented++
			add("tag", suggValue{Name: name, New: true}, t.Confidence)
		}
	}
	if res.Correspondent != nil && corr == nil && !userSet["correspondent"] && strings.TrimSpace(res.Correspondent.Name) != "" {
		if v := find(corrs, res.Correspondent.Name); v != nil {
			add("correspondent", suggValue{Name: v.name, ID: v.id.String()}, res.Correspondent.Confidence)
		} else if name := cleanName(res.Correspondent.Name, 100); name != "" && res.Correspondent.Confidence >= newNameConfidence {
			add("correspondent", suggValue{Name: name, New: true}, res.Correspondent.Confidence)
		}
	}
	if res.DocumentType != nil && typ == nil && !userSet["document_type"] && strings.TrimSpace(res.DocumentType.Name) != "" {
		if v := find(types, res.DocumentType.Name); v != nil {
			add("document_type", suggValue{Name: v.name, ID: v.id.String()}, res.DocumentType.Confidence)
		} else if name := cleanName(res.DocumentType.Name, 60); newTypes && name != "" && res.DocumentType.Confidence >= newNameConfidence {
			add("document_type", suggValue{Name: canonical(tune.CommonTypes, name), New: true}, res.DocumentType.Confidence)
		}
	}
	if res.DocumentDate != nil && docDate == nil && !userSet["document_date"] {
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(*res.DocumentDate)); err == nil && plausibleDate(t) {
			add("document_date", suggValue{Name: t.Format("2006-01-02")}, 0.8)
		}
	}
	if res.Title != nil && !userSet["title"] {
		if t := strings.TrimSpace(*res.Title); t != "" && t != title && len([]rune(t)) <= 200 {
			add("title", suggValue{Name: t}, 0.7)
		}
	}
	for _, cf := range res.CustomFields {
		for _, f := range fields {
			if strings.EqualFold(f.Name, cf.Name) && strings.TrimSpace(cf.Value) != "" {
				add("custom_field", suggValue{Name: strings.TrimSpace(cf.Value), FieldID: f.ID.String(), Field: f.Name}, cf.Confidence)
			}
		}
	}

	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM suggestions WHERE document_id=$1 AND status='pending'`, docID); err != nil {
			return err
		}
		for i := range sugs {
			if _, err := tx.Exec(ctx, `INSERT INTO suggestions (id, document_id, field, value, confidence, provider_id) VALUES ($1,$2,$3,$4,$5,$6)`,
				sugs[i].ID, docID, sugs[i].Field, sugs[i].Value, sugs[i].Confidence, prov.ID); err != nil {
				return err
			}
		}
		if len(sugs) == 0 {
			return nil
		}
		if err := documents.RecordEvent(ctx, tx, docID, "ai_suggested", map[string]any{"count": len(sugs), "model": prov.ChatModel}); err != nil {
			return err
		}
		if applyMode == "auto" {
			var confident []uuid.UUID
			for _, sg := range sugs {
				if sg.Confidence >= autoApplyConfidence {
					confident = append(confident, sg.ID)
				}
			}
			if len(confident) > 0 {
				return s.resolveTx(ctx, tx, docID, spaceID, confident, "applied", "ai", nil)
			}
		}
		return nil
	})
}

func clamp(f float32) float32 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func plausibleDate(t time.Time) bool {
	return t.Year() >= 1950 && t.Before(time.Now().AddDate(5, 0, 0))
}

func jsonList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Service) fieldsOf(ctx context.Context, spaceID uuid.UUID) ([]customfields.Field, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, data_type FROM custom_fields WHERE space_id=$1 AND data_type NOT IN ('document','longtext') ORDER BY lower(name) LIMIT 30`, spaceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (customfields.Field, error) {
		var f customfields.Field
		err := r.Scan(&f.ID, &f.Name, &f.DataType)
		return f, err
	})
}

// examples shows the model how this person filed their latest documents (their own
// edits are the best guide to their habits).
func (s *Service) examples(ctx context.Context, spaceID, exclude uuid.UUID, limit int) string {
	rows, err := s.pool.Query(ctx, `SELECT d.title, coalesce(c.name,''), coalesce(t.name,''),
		coalesce((SELECT string_agg(tg.name, ', ') FROM document_tags dt JOIN tags tg ON tg.id=dt.tag_id WHERE dt.document_id=d.id), '')
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id LEFT JOIN document_types t ON t.id=d.document_type_id
		WHERE d.space_id=$1 AND d.id<>$2 AND d.deleted_at IS NULL AND NOT d.inbox AND d.status='ready' AND (d.correspondent_id IS NOT NULL OR d.document_type_id IS NOT NULL)
		ORDER BY d.updated_at DESC LIMIT $3`, spaceID, exclude, limit)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var title, c, t, tg string
		if rows.Scan(&title, &c, &t, &tg) == nil {
			fmt.Fprintf(&b, "- %q → from %q, type %q, tags [%s]\n", title, c, t, tg)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Resolving suggestions (accept / reject / auto-apply)
// ---------------------------------------------------------------------------

// Pending lists the open suggestions for a document.
func (s *Service) Pending(ctx context.Context, docs *documents.Service, p *auth.Principal, docID uuid.UUID) ([]Suggestion, error) {
	if _, err := docs.Access(ctx, p, docID, spaces.ActView); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, field, value, confidence, status FROM suggestions WHERE document_id=$1 AND status='pending'
		ORDER BY CASE field WHEN 'correspondent' THEN 1 WHEN 'document_type' THEN 2 WHEN 'document_date' THEN 3 WHEN 'tag' THEN 4 WHEN 'title' THEN 5 ELSE 6 END, confidence DESC`, docID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Suggestion, error) {
		var x Suggestion
		err := r.Scan(&x.ID, &x.Field, &x.Value, &x.Confidence, &x.Status)
		return x, err
	})
	if out == nil {
		out = []Suggestion{}
	}
	return out, err
}

// Resolve accepts or rejects suggestions of a document. ids == nil means all pending.
func (s *Service) Resolve(ctx context.Context, docs *documents.Service, p *auth.Principal, docID uuid.UUID, ids []uuid.UUID, accept bool) error {
	a, err := docs.Access(ctx, p, docID, spaces.ActEdit)
	if err != nil {
		return err
	}
	if a.Deleted {
		return apperr.Conflict("in_trash", "Restore this document first")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if ids == nil {
			rows, err := tx.Query(ctx, `SELECT id FROM suggestions WHERE document_id=$1 AND status='pending'`, docID)
			if err != nil {
				return err
			}
			if ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			return nil
		}
		if !accept {
			_, err := tx.Exec(ctx, `UPDATE suggestions SET status='rejected', resolved_at=now(), resolved_by=$3 WHERE document_id=$1 AND id = ANY($2) AND status='pending'`,
				docID, ids, p.UserID)
			return err
		}
		return s.resolveTx(ctx, tx, docID, a.SpaceID, ids, "accepted", "user", &p.UserID)
	})
}

// resolveTx applies the given pending suggestions to the document and marks them with status.
func (s *Service) resolveTx(ctx context.Context, tx pgx.Tx, docID, spaceID uuid.UUID, ids []uuid.UUID, status, source string, by *uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, field, value FROM suggestions WHERE document_id=$1 AND id = ANY($2) AND status='pending' FOR UPDATE`, docID, ids)
	if err != nil {
		return err
	}
	type row struct {
		id    uuid.UUID
		field string
		value suggValue
	}
	var list []row
	for rows.Next() {
		var r row
		var raw []byte
		if err := rows.Scan(&r.id, &r.field, &raw); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &r.value)
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	applied := map[string]any{}
	failed := map[uuid.UUID]bool{}
	var tagNames []string
	for _, r := range list {
		var err error
		switch r.field {
		case "tag":
			var tid uuid.UUID
			if r.value.New || r.value.ID == "" {
				tid, err = taxonomy.EnsureByName(ctx, tx, taxonomy.Tags, spaceID, r.value.Name)
			} else {
				tid, err = uuid.Parse(r.value.ID)
			}
			if err != nil {
				break
			}
			_, err = tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id, source) SELECT $1, id, 'ai' FROM tags WHERE id=$2 AND space_id=$3 ON CONFLICT DO NOTHING`, docID, tid, spaceID)
			tagNames = append(tagNames, r.value.Name)
		case "correspondent", "document_type":
			kind, col := taxonomy.Correspondents, "correspondent_id"
			if r.field == "document_type" {
				kind, col = taxonomy.DocumentTypes, "document_type_id"
			}
			var id uuid.UUID
			if r.value.New || r.value.ID == "" {
				id, err = taxonomy.EnsureByName(ctx, tx, kind, spaceID, r.value.Name)
			} else {
				id, err = uuid.Parse(r.value.ID)
			}
			if err == nil {
				_, err = tx.Exec(ctx, `UPDATE documents SET `+col+`=$2 WHERE id=$1`, docID, id)
			}
			if err == nil {
				err = setFieldSource(ctx, tx, docID, strings.TrimSuffix(r.field, "_id"), source, by)
				applied[r.field] = r.value.Name
			}
		case "document_date":
			_, err = tx.Exec(ctx, `UPDATE documents SET document_date=$2::date WHERE id=$1`, docID, r.value.Name)
			if err == nil {
				err = setFieldSource(ctx, tx, docID, "document_date", source, by)
				applied["document_date"] = r.value.Name
			}
		case "title":
			_, err = tx.Exec(ctx, `UPDATE documents SET title=$2 WHERE id=$1`, docID, r.value.Name)
			if err == nil {
				err = setFieldSource(ctx, tx, docID, "title", source, by)
				applied["title"] = r.value.Name
			}
		case "custom_field":
			// A value the field can't hold is skipped, not fatal: roll back just that attempt.
			sp, serr := tx.Begin(ctx)
			if serr != nil {
				return serr
			}
			val, _ := json.Marshal(r.value.Name)
			if _, cerr := customfields.SetValues(ctx, sp, docID, spaceID, map[string]json.RawMessage{r.value.FieldID: val}, source); cerr != nil {
				_ = sp.Rollback(ctx)
				failed[r.id] = true
				continue
			}
			err = sp.Commit(ctx)
			applied[r.value.Field] = r.value.Name
		}
		if err != nil {
			return err
		}
	}
	if len(tagNames) > 0 {
		applied["tags"] = tagNames
	}
	for _, r := range list {
		st := status
		if failed[r.id] {
			st = "rejected"
		}
		if _, err := tx.Exec(ctx, `UPDATE suggestions SET status=$2, resolved_at=now(), resolved_by=$3 WHERE id=$1`, r.id, st, by); err != nil {
			return err
		}
	}
	if len(applied) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE documents SET version=version+1 WHERE id=$1`, docID); err != nil {
			return err
		}
		action := "ai_accepted"
		if status == "applied" {
			action = "ai_applied"
		}
		if err := documents.RecordEvent(ctx, tx, docID, action, applied); err != nil {
			return err
		}
		return documents.ReindexMeta(ctx, tx, docID)
	}
	return nil
}

func setFieldSource(ctx context.Context, q db.Querier, docID uuid.UUID, field, source string, by *uuid.UUID) error {
	_, err := q.Exec(ctx, `INSERT INTO field_sources (document_id, field, source, set_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT (document_id, field) DO UPDATE SET source=excluded.source, set_by=excluded.set_by, set_at=now()`, docID, field, source, by)
	return err
}

// SpaceStats is how often a space's suggestions were taken (last 90 days).
type SpaceStats struct {
	Accepted int     `json:"accepted"`
	Rejected int     `json:"rejected"`
	Pending  int     `json:"pending"`
	Rate     float64 `json:"accept_rate"` // 0..1, 0 when nothing was decided yet
}

func (s *Service) Stats(ctx context.Context, p *auth.Principal, spaceID uuid.UUID) (*SpaceStats, error) {
	if _, err := s.spaces.Require(ctx, p, spaceID, spaces.ActView); err != nil {
		return nil, err
	}
	var st SpaceStats
	err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE s.status IN ('accepted','applied')), count(*) FILTER (WHERE s.status='rejected'),
		count(*) FILTER (WHERE s.status='pending') FROM suggestions s JOIN documents d ON d.id=s.document_id
		WHERE d.space_id=$1 AND s.created_at > now() - interval '90 days'`, spaceID).Scan(&st.Accepted, &st.Rejected, &st.Pending)
	if n := st.Accepted + st.Rejected; n > 0 {
		st.Rate = float64(st.Accepted) / float64(n)
	}
	return &st, err
}
