// Package workflows runs "when this happens to a document that looks like this, do
// that" rules: tag, file, move, fill in fields, notify, call a webhook. They run in the
// background after a document is added, processed or edited, or daily on a schedule.
// Changes made by workflows never trigger workflows themselves, so rules can't loop.
package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/opt"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/taxonomy"
)

// Action is one step of a workflow. Which fields matter depends on Type:
// add_tags / remove_tags (Names), set_correspondent / set_document_type (Name),
// set_field (FieldID, Value), set_inbox (Value true/false), move_to_space (SpaceID),
// notify (To, Title, Message), webhook (URL), run_ai.
type Action struct {
	Type    string          `json:"type"`
	Names   []string        `json:"names,omitempty"`
	Name    string          `json:"name,omitempty"`
	FieldID *uuid.UUID      `json:"field_id,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	SpaceID *uuid.UUID      `json:"space_id,omitempty"`
	To      string          `json:"to,omitempty"` // owner | space_owners | space_members
	Title   string          `json:"title,omitempty"`
	Message string          `json:"message,omitempty"`
	URL     string          `json:"url,omitempty"`
}

type Workflow struct {
	ID           uuid.UUID       `json:"id"`
	SpaceID      uuid.UUID       `json:"space_id"`
	Name         string          `json:"name"`
	Enabled      bool            `json:"enabled"`
	Trigger      string          `json:"trigger"` // added | processed | updated | schedule
	ScheduleTime string          `json:"schedule_time"`
	Conditions   json.RawMessage `json:"conditions"`
	Actions      []Action        `json:"actions"`
	LastRunAt    *time.Time      `json:"last_run_at"`
	RunCount     int             `json:"run_count"`
}

type Input struct {
	SpaceID      *uuid.UUID      `json:"space_id"`
	Name         *string         `json:"name"`
	Enabled      *bool           `json:"enabled"`
	Trigger      *string         `json:"trigger"`
	ScheduleTime *string         `json:"schedule_time"`
	Conditions   json.RawMessage `json:"conditions"`
	Actions      *[]Action       `json:"actions"`
}

type Run struct {
	ID         uuid.UUID  `json:"id"`
	DocumentID *uuid.UUID `json:"document_id"`
	Title      string     `json:"document_title"`
	Trigger    string     `json:"trigger"`
	Status     string     `json:"status"`
	Summary    string     `json:"summary"`
	RanAt      time.Time  `json:"ran_at"`
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
	search *search.Service
	docs   *documents.Service
	queue  *jobs.Queue
	log    *slog.Logger
	// Webhook posts JSON to a user-defined address (set by the app: SSRF-guarded).
	Webhook func(ctx context.Context, url string, payload any) error
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service, se *search.Service, docs *documents.Service, q *jobs.Queue, log *slog.Logger) *Service {
	return &Service{pool: pool, spaces: sp, search: se, docs: docs, queue: q, log: log}
}

var (
	triggers   = []string{"added", "processed", "updated", "schedule"}
	actionKind = []string{"add_tags", "remove_tags", "set_correspondent", "set_document_type", "set_field", "set_inbox", "move_to_space", "notify", "webhook", "run_ai"}
	hhmm       = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

func (s *Service) validateActions(ctx context.Context, spaceID uuid.UUID, p *auth.Principal, actions []Action) error {
	var v apperr.Validation
	if len(actions) == 0 {
		v.Add("actions", "Add at least one action")
	}
	if len(actions) > 20 {
		v.Add("actions", "At most 20 actions")
	}
	for i, a := range actions {
		f := fmt.Sprintf("actions.%d", i)
		if !slices.Contains(actionKind, a.Type) {
			v.Add(f, "Unknown action")
			continue
		}
		switch a.Type {
		case "add_tags", "remove_tags":
			if len(a.Names) == 0 {
				v.Add(f, "Choose at least one tag")
			}
		case "set_correspondent", "set_document_type":
			if strings.TrimSpace(a.Name) == "" {
				v.Add(f, "Enter a name")
			}
		case "set_field":
			if a.FieldID == nil {
				v.Add(f, "Choose a field")
			} else if err := s.pool.QueryRow(ctx, `SELECT 1 FROM custom_fields WHERE id=$1 AND space_id=$2`, *a.FieldID, spaceID).Scan(new(int)); err != nil {
				v.Add(f, "That field doesn't exist in this space")
			}
		case "set_inbox":
			var b bool
			if json.Unmarshal(a.Value, &b) != nil {
				v.Add(f, "Choose yes or no")
			}
		case "move_to_space":
			if a.SpaceID == nil || *a.SpaceID == spaceID {
				v.Add(f, "Choose a different space")
			} else if _, err := s.spaces.Require(ctx, p, *a.SpaceID, spaces.ActEdit); err != nil {
				v.Add(f, "You can't add documents to that space")
			}
		case "notify":
			if !slices.Contains([]string{"owner", "space_owners", "space_members"}, a.To) {
				v.Add(f, "Choose who to tell")
			}
			if utf8.RuneCountInString(a.Title) > 200 || utf8.RuneCountInString(a.Message) > 1000 {
				v.Add(f, "Message is too long")
			}
		case "webhook":
			if u, err := url.Parse(a.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				v.Add(f, "Enter a valid web address")
			}
		}
	}
	return v.Err()
}

func (s *Service) validateConditions(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var q search.Query
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&q); err != nil {
		return apperr.Invalid("conditions", "Invalid conditions")
	}
	return nil
}

const cols = `w.id, w.space_id, w.name, w.enabled, w.trigger, w.schedule_time, w.conditions, w.actions, w.last_run_at,
	(SELECT count(*) FROM workflow_runs r WHERE r.workflow_id=w.id AND r.status='done')`

func scan(row pgx.Row) (*Workflow, error) {
	var w Workflow
	var acts []byte
	if err := row.Scan(&w.ID, &w.SpaceID, &w.Name, &w.Enabled, &w.Trigger, &w.ScheduleTime, &w.Conditions, &acts, &w.LastRunAt, &w.RunCount); err != nil {
		if db.IsNoRows(err) {
			return nil, apperr.NotFound("Workflow")
		}
		return nil, err
	}
	_ = json.Unmarshal(acts, &w.Actions)
	if w.Actions == nil {
		w.Actions = []Action{}
	}
	return &w, nil
}

func (s *Service) List(ctx context.Context, p *auth.Principal, spaceID *uuid.UUID) ([]*Workflow, error) {
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
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM workflows w WHERE w.space_id = ANY($1) ORDER BY w.sort_order, lower(w.name)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Workflow{}
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Service) get(ctx context.Context, id uuid.UUID) (*Workflow, error) {
	return scan(s.pool.QueryRow(ctx, `SELECT `+cols+` FROM workflows w WHERE w.id=$1`, id))
}

func (s *Service) authorize(ctx context.Context, p *auth.Principal, id uuid.UUID, act spaces.Action) (*Workflow, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.spaces.Require(ctx, p, w.SpaceID, act); err != nil {
		if apperr.IsKind(err, apperr.KindNotFound) {
			return nil, apperr.NotFound("Workflow")
		}
		return nil, err
	}
	return w, nil
}

func (s *Service) Create(ctx context.Context, p *auth.Principal, in Input) (*Workflow, error) {
	if in.SpaceID == nil {
		return nil, apperr.Invalid("space_id", "Choose a space")
	}
	if _, err := s.spaces.Require(ctx, p, *in.SpaceID, spaces.ActManage); err != nil {
		return nil, err
	}
	var v apperr.Validation
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" || utf8.RuneCountInString(name) > 100 {
		v.Add("name", "Name must be 1–100 characters")
	}
	trigger := ""
	if in.Trigger != nil {
		trigger = *in.Trigger
	}
	if !slices.Contains(triggers, trigger) {
		v.Add("trigger", "Choose when the workflow runs")
	}
	sched := "03:00"
	if in.ScheduleTime != nil {
		sched = *in.ScheduleTime
	}
	if !hhmm.MatchString(sched) {
		v.Add("schedule_time", "Use a time like 03:00")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.validateConditions(in.Conditions); err != nil {
		return nil, err
	}
	var actions []Action
	if in.Actions != nil {
		actions = *in.Actions
	}
	if err := s.validateActions(ctx, *in.SpaceID, p, actions); err != nil {
		return nil, err
	}
	cond := in.Conditions
	if len(cond) == 0 {
		cond = json.RawMessage(`{}`)
	}
	acts, _ := json.Marshal(actions)
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO workflows (id, space_id, name, enabled, trigger, schedule_time, conditions, actions, created_by,
		sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, (SELECT coalesce(max(sort_order),0)+1 FROM workflows WHERE space_id=$2))`,
		id, *in.SpaceID, name, in.Enabled == nil || *in.Enabled, trigger, sched, []byte(cond), acts, p.UserID); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) Update(ctx context.Context, p *auth.Principal, id uuid.UUID, in Input) (*Workflow, error) {
	w, err := s.authorize(ctx, p, id, spaces.ActManage)
	if err != nil {
		return nil, err
	}
	var v apperr.Validation
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		if n == "" || utf8.RuneCountInString(n) > 100 {
			v.Add("name", "Name must be 1–100 characters")
		}
	}
	if in.Trigger != nil && !slices.Contains(triggers, *in.Trigger) {
		v.Add("trigger", "Choose when the workflow runs")
	}
	if in.ScheduleTime != nil && !hhmm.MatchString(*in.ScheduleTime) {
		v.Add("schedule_time", "Use a time like 03:00")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.validateConditions(in.Conditions); err != nil {
		return nil, err
	}
	var acts []byte
	if in.Actions != nil {
		if err := s.validateActions(ctx, w.SpaceID, p, *in.Actions); err != nil {
			return nil, err
		}
		acts, _ = json.Marshal(*in.Actions)
	}
	var cond []byte
	if len(in.Conditions) > 0 {
		cond = in.Conditions
	}
	if _, err := s.pool.Exec(ctx, `UPDATE workflows SET name=coalesce($2,name), enabled=coalesce($3,enabled), trigger=coalesce($4,trigger),
		schedule_time=coalesce($5,schedule_time), conditions=coalesce($6,conditions), actions=coalesce($7,actions), updated_at=now() WHERE id=$1`,
		id, in.Name, in.Enabled, in.Trigger, in.ScheduleTime, cond, acts); err != nil {
		return nil, err
	}
	return s.get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if _, err := s.authorize(ctx, p, id, spaces.ActManage); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM workflows WHERE id=$1`, id)
	return err
}

func (s *Service) Runs(ctx context.Context, p *auth.Principal, id uuid.UUID) ([]Run, error) {
	if _, err := s.authorize(ctx, p, id, spaces.ActView); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.document_id, coalesce(d.title,''), r.trigger, r.status, r.summary, r.ran_at
		FROM workflow_runs r LEFT JOIN documents d ON d.id=r.document_id WHERE r.workflow_id=$1 ORDER BY r.ran_at DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) {
		var x Run
		err := r.Scan(&x.ID, &x.DocumentID, &x.Title, &x.Trigger, &x.Status, &x.Summary, &x.RanAt)
		return x, err
	})
	if out == nil {
		out = []Run{}
	}
	return out, err
}

// TestResult is a dry run: would the workflow fire for this document, and what would it do?
type TestResult struct {
	Matches bool     `json:"matches"`
	Steps   []string `json:"steps"`
}

func (s *Service) Test(ctx context.Context, p *auth.Principal, id, docID uuid.UUID) (*TestResult, error) {
	w, err := s.authorize(ctx, p, id, spaces.ActManage)
	if err != nil {
		return nil, err
	}
	a, err := s.docs.Access(ctx, p, docID, spaces.ActView)
	if err != nil {
		return nil, err
	}
	if a.SpaceID != w.SpaceID {
		return nil, apperr.Invalid("document_id", "Pick a document from this workflow's space")
	}
	var q search.Query
	_ = json.Unmarshal(w.Conditions, &q)
	ok, err := s.search.Matches(ctx, w.SpaceID, docID, q)
	if err != nil {
		return nil, err
	}
	res := &TestResult{Matches: ok, Steps: []string{}}
	for _, act := range w.Actions {
		res.Steps = append(res.Steps, Describe(act))
	}
	return res, nil
}

// Describe is a plain-language sentence for an action.
func Describe(a Action) string {
	switch a.Type {
	case "add_tags":
		return "Add tags: " + strings.Join(a.Names, ", ")
	case "remove_tags":
		return "Remove tags: " + strings.Join(a.Names, ", ")
	case "set_correspondent":
		return "Set the sender to " + a.Name
	case "set_document_type":
		return "Set the type to " + a.Name
	case "set_field":
		return "Fill in a custom field"
	case "set_inbox":
		if string(a.Value) == "true" {
			return "Send it to the Inbox for review"
		}
		return "Mark it as reviewed"
	case "move_to_space":
		return "Move it to another space"
	case "notify":
		return "Notify " + strings.ReplaceAll(a.To, "_", " ")
	case "webhook":
		return "Call a webhook"
	case "run_ai":
		return "Ask the AI to suggest organisation"
	}
	return a.Type
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

type RunWorker struct {
	river.WorkerDefaults[jobs.WorkflowArgs]
	S *Service
}

func (w *RunWorker) Work(ctx context.Context, job *river.Job[jobs.WorkflowArgs]) error {
	return w.S.RunTrigger(ctx, job.Args.DocumentID, job.Args.Trigger)
}

type docInfo struct {
	ID            uuid.UUID
	SpaceID       uuid.UUID
	Title         string
	OwnerID       *uuid.UUID
	Correspondent string
	Date          string
}

func (s *Service) loadDoc(ctx context.Context, q db.Querier, id uuid.UUID) (*docInfo, bool, error) {
	var d docInfo
	var deleted bool
	err := q.QueryRow(ctx, `SELECT d.id, d.space_id, d.title, d.owner_id, coalesce(c.name,''), coalesce(to_char(d.document_date,'YYYY-MM-DD'),''), d.deleted_at IS NOT NULL
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id WHERE d.id=$1`, id).
		Scan(&d.ID, &d.SpaceID, &d.Title, &d.OwnerID, &d.Correspondent, &d.Date, &deleted)
	if db.IsNoRows(err) {
		return nil, true, nil
	}
	return &d, deleted, err
}

// RunTrigger applies every enabled workflow of the document's space for this trigger.
func (s *Service) RunTrigger(ctx context.Context, docID uuid.UUID, trigger string) error {
	d, gone, err := s.loadDoc(ctx, s.pool, docID)
	if err != nil || gone {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM workflows w WHERE w.space_id=$1 AND w.enabled AND w.trigger=$2 ORDER BY w.sort_order, w.created_at`, d.SpaceID, trigger)
	if err != nil {
		return err
	}
	var list []*Workflow
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, w)
	}
	rows.Close()
	for _, w := range list {
		moved, err := s.runOne(ctx, w, d, trigger)
		if err != nil {
			s.log.Warn("workflow failed", "workflow", w.Name, "document", docID, "err", err)
		}
		if moved {
			break // the document left this space; the rest of its workflows no longer apply
		}
	}
	return nil
}

func (s *Service) record(ctx context.Context, q db.Querier, w *Workflow, docID uuid.UUID, trigger, status, summary string) {
	_, _ = q.Exec(ctx, `INSERT INTO workflow_runs (id, workflow_id, document_id, trigger, status, summary) VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.Must(uuid.NewV7()), w.ID, docID, trigger, status, summary)
	_, _ = q.Exec(ctx, `UPDATE workflows SET last_run_at=now() WHERE id=$1`, w.ID)
}

type after struct { // work to do once the transaction has committed
	events   []jobs.Event
	webhooks []string
	ai       bool
	move     *uuid.UUID
}

// runOne checks the conditions and applies the actions. Reports whether the document moved.
func (s *Service) runOne(ctx context.Context, w *Workflow, d *docInfo, trigger string) (moved bool, err error) {
	if trigger == "updated" { // a short guard against bursts of edits
		var recent bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow_runs WHERE workflow_id=$1 AND document_id=$2 AND ran_at > now() - interval '5 seconds')`, w.ID, d.ID).Scan(&recent); err == nil && recent {
			return false, nil
		}
	}
	var q search.Query
	_ = json.Unmarshal(w.Conditions, &q)
	ok, err := s.search.Matches(ctx, d.SpaceID, d.ID, q)
	if err != nil || !ok {
		return false, err
	}
	var todo after
	var summary []string
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM documents WHERE id=$1 FOR UPDATE`, d.ID); err != nil {
			return err
		}
		userSet := map[string]bool{}
		rows, err := tx.Query(ctx, `SELECT field FROM field_sources WHERE document_id=$1 AND source='user'`, d.ID)
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
		changed := false
		for _, a := range w.Actions {
			did, err := s.apply(ctx, tx, w, d, a, userSet, &todo)
			if err != nil {
				summary = append(summary, Describe(a)+" — failed: "+err.Error())
				continue
			}
			if did {
				changed = true
				summary = append(summary, Describe(a))
			}
		}
		if changed {
			if _, err := tx.Exec(ctx, `UPDATE documents SET version=version+1 WHERE id=$1`, d.ID); err != nil {
				return err
			}
			if err := documents.ReindexMeta(ctx, tx, d.ID); err != nil {
				return err
			}
		}
		if err := documents.RecordEvent(ctx, tx, d.ID, "workflow_ran", map[string]any{"workflow": w.Name, "did": summary}); err != nil {
			return err
		}
		status := "done"
		if len(summary) == 0 {
			status = "skipped"
		}
		s.record(ctx, tx, w, d.ID, trigger, status, strings.Join(summary, "; "))
		return nil
	})
	if err != nil {
		s.record(ctx, s.pool, w, d.ID, trigger, "failed", err.Error())
		return false, err
	}
	for _, e := range todo.events {
		_ = s.queue.Emit(ctx, e)
	}
	for _, u := range todo.webhooks {
		if s.Webhook != nil {
			payload := map[string]any{"workflow": w.Name, "trigger": trigger, "document": map[string]any{"id": d.ID, "title": d.Title, "space_id": d.SpaceID}}
			if err := s.Webhook(ctx, u, payload); err != nil {
				s.log.Warn("workflow webhook failed", "workflow", w.Name, "err", err)
			}
		}
	}
	if todo.ai {
		_ = s.queue.Insert(ctx, jobs.AIArgs{DocumentID: d.ID, Classify: true}, &river.InsertOpts{Priority: jobs.PriorityBackground, MaxAttempts: 2})
	}
	if todo.move != nil {
		// Moving goes through the normal update path (tags, correspondent and type are mapped by
		// name) as the system, so it can't trigger more workflows.
		if _, err := s.docs.Update(ctx, auth.System(), d.ID, documents.UpdateInput{SpaceID: opt.Of(*todo.move)}, nil); err != nil {
			s.log.Warn("workflow move failed", "workflow", w.Name, "err", err)
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

func render(tmpl string, d *docInfo) string {
	r := strings.NewReplacer("{{title}}", d.Title, "{{correspondent}}", d.Correspondent, "{{date}}", d.Date)
	return r.Replace(tmpl)
}

func (s *Service) apply(ctx context.Context, tx pgx.Tx, w *Workflow, d *docInfo, a Action, userSet map[string]bool, todo *after) (bool, error) {
	switch a.Type {
	case "add_tags":
		did := false
		for _, name := range a.Names {
			id, err := taxonomy.EnsureByName(ctx, tx, taxonomy.Tags, d.SpaceID, strings.TrimSpace(name))
			if err != nil {
				return did, err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO document_tags (document_id, tag_id, source) VALUES ($1,$2,'workflow') ON CONFLICT DO NOTHING`, d.ID, id)
			if err != nil {
				return did, err
			}
			did = did || tag.RowsAffected() > 0
		}
		return did, nil
	case "remove_tags":
		lower := make([]string, len(a.Names))
		for i, n := range a.Names {
			lower[i] = strings.ToLower(strings.TrimSpace(n))
		}
		tag, err := tx.Exec(ctx, `DELETE FROM document_tags WHERE document_id=$1 AND tag_id IN (SELECT id FROM tags WHERE space_id=$2 AND lower(name) = ANY($3))`, d.ID, d.SpaceID, lower)
		return err == nil && tag.RowsAffected() > 0, err
	case "set_correspondent", "set_document_type":
		kind, col, field := taxonomy.Correspondents, "correspondent_id", "correspondent"
		if a.Type == "set_document_type" {
			kind, col, field = taxonomy.DocumentTypes, "document_type_id", "document_type"
		}
		if userSet[field] {
			return false, nil // a person's choice wins
		}
		id, err := taxonomy.EnsureByName(ctx, tx, kind, d.SpaceID, strings.TrimSpace(a.Name))
		if err != nil {
			return false, err
		}
		tag, err := tx.Exec(ctx, `UPDATE documents SET `+col+`=$2 WHERE id=$1 AND `+col+` IS DISTINCT FROM $2`, d.ID, id)
		if err != nil || tag.RowsAffected() == 0 {
			return false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO field_sources (document_id, field, source) VALUES ($1,$2,'workflow')
			ON CONFLICT (document_id, field) DO UPDATE SET source='workflow', set_at=now()`, d.ID, field)
		return true, err
	case "set_field":
		sp, err := tx.Begin(ctx)
		if err != nil {
			return false, err
		}
		if _, err := customfields.SetValues(ctx, sp, d.ID, d.SpaceID, map[string]json.RawMessage{a.FieldID.String(): a.Value}, "workflow"); err != nil {
			_ = sp.Rollback(ctx)
			return false, err
		}
		return true, sp.Commit(ctx)
	case "set_inbox":
		var b bool
		_ = json.Unmarshal(a.Value, &b)
		tag, err := tx.Exec(ctx, `UPDATE documents SET inbox=$2 WHERE id=$1 AND inbox<>$2`, d.ID, b)
		return err == nil && tag.RowsAffected() > 0, err
	case "move_to_space":
		todo.move = a.SpaceID
		return true, nil
	case "notify":
		var rec []uuid.UUID
		switch a.To {
		case "owner":
			if d.OwnerID != nil {
				rec = []uuid.UUID{*d.OwnerID}
			}
		case "space_owners", "space_members":
			role := "('owner')"
			if a.To == "space_members" {
				role = "('owner','editor','viewer')"
			}
			rows, err := tx.Query(ctx, `SELECT user_id FROM space_members WHERE space_id=$1 AND role IN `+role, d.SpaceID)
			if err != nil {
				return false, err
			}
			if rec, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return false, err
			}
		}
		if len(rec) == 0 {
			return false, nil
		}
		title := render(a.Title, d)
		if strings.TrimSpace(title) == "" {
			title = "Workflow “" + w.Name + "” ran on “" + d.Title + "”"
		}
		id := d.ID
		todo.events = append(todo.events, jobs.Event{Type: "workflow.notice", Title: title, Body: render(a.Message, d), Link: "/documents/" + d.ID.String(),
			Severity: "info", Recipients: rec, DocumentID: &id, SpaceID: &d.SpaceID})
		return true, nil
	case "webhook":
		todo.webhooks = append(todo.webhooks, a.URL)
		return true, nil
	case "run_ai":
		todo.ai = true
		return true, nil
	}
	return false, fmt.Errorf("unknown action %q", a.Type)
}

// ---------------------------------------------------------------------------
// Scheduled workflows
// ---------------------------------------------------------------------------

type ScheduleWorker struct {
	river.WorkerDefaults[jobs.WorkflowScheduleArgs]
	S *Service
}

func (w *ScheduleWorker) Timeout(*river.Job[jobs.WorkflowScheduleArgs]) time.Duration {
	return 30 * time.Minute
}

func (w *ScheduleWorker) Work(ctx context.Context, _ *river.Job[jobs.WorkflowScheduleArgs]) error {
	return w.S.RunScheduled(ctx, time.Now())
}

// RunScheduled runs workflows whose daily time has passed today and that haven't run
// since: each matching document that hasn't been handled by that workflow yet.
func (s *Service) RunScheduled(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, `SELECT `+cols+` FROM workflows w WHERE w.enabled AND w.trigger='schedule'`)
	if err != nil {
		return err
	}
	var list []*Workflow
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, w)
	}
	rows.Close()
	for _, w := range list {
		t, err := time.Parse("15:04", w.ScheduleTime)
		if err != nil {
			continue
		}
		due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if now.Before(due) || (w.LastRunAt != nil && !w.LastRunAt.Before(due)) {
			continue
		}
		var q search.Query
		_ = json.Unmarshal(w.Conditions, &q)
		ids, err := s.search.MatchAll(ctx, w.SpaceID, q, 500)
		if err != nil {
			s.log.Warn("scheduled workflow search failed", "workflow", w.Name, "err", err)
			continue
		}
		for _, id := range ids {
			var seen bool
			if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow_runs WHERE workflow_id=$1 AND document_id=$2 AND status IN ('done','skipped'))`, w.ID, id).Scan(&seen); err != nil || seen {
				continue
			}
			d, gone, err := s.loadDoc(ctx, s.pool, id)
			if err != nil || gone {
				continue
			}
			if _, err := s.runOne(ctx, w, d, "schedule"); err != nil {
				s.log.Warn("scheduled workflow failed", "workflow", w.Name, "document", id, "err", err)
			}
		}
		_, _ = s.pool.Exec(ctx, `UPDATE workflows SET last_run_at=now() WHERE id=$1`, w.ID)
	}
	return nil
}
