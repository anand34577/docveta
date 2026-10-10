package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/storage"
)

// ProtocolVersion is the worker protocol version implemented by this core. The core
// supports ProtocolVersion and ProtocolVersion-1.
const ProtocolVersion = 1

const workerTokenPrefix = "dvt_wrk"

// Capability is what a worker advertises for one task type.
type Capability struct {
	TaskType        string   `json:"task_type"` // ocr | archive | convert
	Engine          string   `json:"engine"`
	EngineVersion   string   `json:"engine_version,omitempty"`
	Languages       []string `json:"languages,omitempty"`
	InputMime       []string `json:"input_mime,omitempty"`
	Outputs         []string `json:"outputs,omitempty"`
	MaxPagesPerTask int      `json:"max_pages_per_task,omitempty"`
	Concurrency     int      `json:"concurrency,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type Worker struct {
	ID              uuid.UUID    `json:"id"`
	Name            string       `json:"name"`
	TokenPrefix     string       `json:"token_prefix"`
	Enabled         bool         `json:"enabled"`
	ProtocolVersion int          `json:"protocol_version"`
	Version         string       `json:"version"`
	Host            string       `json:"host"`
	Capabilities    []Capability `json:"capabilities"`
	LastSeenAt      *time.Time   `json:"last_seen_at"`
	Online          bool         `json:"online"`
	ActiveTasks     int          `json:"active_tasks"`
	Done24h         int          `json:"done_24h"`
	Failed24h       int          `json:"failed_24h"`
	CreatedAt       time.Time    `json:"created_at"`
}

var workerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// onlineWindow: a worker that long-polls at least every 30 s is considered online.
const onlineWindow = 2 * time.Minute

func (s *Service) ListWorkers(ctx context.Context, p *auth.Principal) ([]Worker, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	rows, err := s.pool.Query(ctx, `SELECT w.id, w.name, w.token_prefix, w.enabled, w.protocol_version, w.version, w.host, w.capabilities,
		w.last_seen_at, w.created_at,
		(SELECT count(*) FROM processing_tasks t WHERE t.worker_id=w.id AND t.status='leased'),
		(SELECT count(*) FROM processing_tasks t WHERE t.worker_id=w.id AND t.status='done' AND t.finished_at > now()-interval '1 day'),
		(SELECT count(*) FROM processing_tasks t WHERE t.worker_id=w.id AND t.status='failed' AND t.finished_at > now()-interval '1 day')
		FROM workers w ORDER BY w.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Worker, error) {
		var w Worker
		var caps []byte
		err := r.Scan(&w.ID, &w.Name, &w.TokenPrefix, &w.Enabled, &w.ProtocolVersion, &w.Version, &w.Host, &caps, &w.LastSeenAt,
			&w.CreatedAt, &w.ActiveTasks, &w.Done24h, &w.Failed24h)
		if err == nil {
			_ = json.Unmarshal(caps, &w.Capabilities)
			if w.Capabilities == nil {
				w.Capabilities = []Capability{}
			}
			w.Online = w.LastSeenAt != nil && time.Since(*w.LastSeenAt) < onlineWindow
		}
		return w, err
	})
}

// CreateWorker registers a worker and returns its token (shown once).
func (s *Service) CreateWorker(ctx context.Context, p *auth.Principal, name string) (*Worker, string, error) {
	if !p.Admin() {
		return nil, "", apperr.Forbidden("")
	}
	name = strings.TrimSpace(name)
	if !workerNameRe.MatchString(name) {
		return nil, "", apperr.Invalid("name", "Use letters, numbers, dots, dashes or underscores (e.g. rk3588-npu-1)")
	}
	token, prefix := crypto.NewToken(workerTokenPrefix)
	id := uuid.Must(uuid.NewV7())
	_, err := s.pool.Exec(ctx, `INSERT INTO workers (id, name, token_prefix, token_hash) VALUES ($1,$2,$3,$4)`, id, name, prefix, crypto.HashToken(token))
	if db.IsUniqueViolation(err) {
		return nil, "", apperr.Conflict("name_taken", "A worker with this name already exists")
	}
	if err != nil {
		return nil, "", err
	}
	return &Worker{ID: id, Name: name, TokenPrefix: prefix, Enabled: true, Capabilities: []Capability{}, CreatedAt: time.Now()}, token, nil
}

func (s *Service) RotateWorkerToken(ctx context.Context, p *auth.Principal, id uuid.UUID) (string, error) {
	if !p.Admin() {
		return "", apperr.Forbidden("")
	}
	token, prefix := crypto.NewToken(workerTokenPrefix)
	tag, err := s.pool.Exec(ctx, `UPDATE workers SET token_prefix=$2, token_hash=$3 WHERE id=$1`, id, prefix, crypto.HashToken(token))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", apperr.NotFound("Worker")
	}
	return token, nil
}

// IssueWorkerToken creates the worker called name, or gives it a new token if it exists.
// For workers Docveta sets up itself (the bundled engine, Docker enrollment): no admin
// copies a token, and a restarted worker simply gets a fresh one.
func (s *Service) IssueWorkerToken(ctx context.Context, name string) (string, error) {
	sys := auth.System()
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM workers WHERE name=$1`, name).Scan(&id)
	if db.IsNoRows(err) {
		_, token, err := s.CreateWorker(ctx, sys, name)
		return token, err
	}
	if err != nil {
		return "", err
	}
	return s.RotateWorkerToken(ctx, sys, id)
}

func (s *Service) SetWorkerEnabled(ctx context.Context, p *auth.Principal, id uuid.UUID, enabled bool) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE workers SET enabled=$2 WHERE id=$1`, id, enabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound("Worker")
		}
		if !enabled {
			return s.releaseWorkerTasks(ctx, tx, id)
		}
		return nil
	})
}

func (s *Service) DeleteWorker(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.releaseWorkerTasks(ctx, tx, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM workers WHERE id=$1`, id)
		if err == nil && tag.RowsAffected() == 0 {
			return apperr.NotFound("Worker")
		}
		return err
	})
}

// releaseWorkerTasks returns a worker's leased tasks to the queue immediately.
func (s *Service) releaseWorkerTasks(ctx context.Context, tx pgx.Tx, workerID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='queued', lease_id=NULL, worker_id=NULL, lease_expires_at=NULL,
		attempt=greatest(attempt-1, 0), updated_at=now() WHERE worker_id=$1 AND status='leased'`, workerID)
	if err == nil {
		defer s.signal()
	}
	return err
}

// AuthenticateWorker resolves a worker token.
func (s *Service) AuthenticateWorker(ctx context.Context, token string) (*auth.Principal, error) {
	if !strings.HasPrefix(token, workerTokenPrefix+"_") {
		return nil, nil
	}
	var id uuid.UUID
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT id, enabled FROM workers WHERE token_hash=$1`, crypto.HashToken(token)).Scan(&id, &enabled)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, apperr.Forbidden("This worker is disabled in Docveta")
	}
	return &auth.Principal{Kind: auth.KindWorker, WorkerID: id}, nil
}

// ---------------------------------------------------------------------------
// Protocol
// ---------------------------------------------------------------------------

type HelloRequest struct {
	Protocol []int `json:"protocol"`
	Worker   struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Host    string `json:"host"`
	} `json:"worker"`
	Capabilities []Capability `json:"capabilities"`
}

type HelloResponse struct {
	WorkerID          uuid.UUID `json:"worker_id"`
	Protocol          int       `json:"protocol"`
	HeartbeatInterval int       `json:"heartbeat_interval_seconds"`
	LeaseTTL          int       `json:"lease_ttl_seconds"`
	MaxLeaseWait      int       `json:"max_lease_wait_seconds"`
	ServerVersion     string    `json:"server_version"`
}

// ServerVersion is set by main.
var ServerVersion = "dev"

func (s *Service) Hello(ctx context.Context, p *auth.Principal, req HelloRequest) (*HelloResponse, error) {
	if p == nil || p.Kind != auth.KindWorker {
		return nil, apperr.Unauthorized("Worker token required")
	}
	proto := 0
	for _, v := range req.Protocol {
		if v <= ProtocolVersion && v >= ProtocolVersion-1 && v > proto {
			proto = v
		}
	}
	if proto == 0 {
		return nil, &apperr.Error{Kind: apperr.KindConflict, Code: "protocol_unsupported",
			Msg: fmt.Sprintf("This Docveta server speaks worker protocol %d; upgrade the worker", ProtocolVersion)}
	}
	var v apperr.Validation
	for i, c := range req.Capabilities {
		switch c.TaskType {
		case "ocr", "archive", "convert":
		default:
			v.Add(fmt.Sprintf("capabilities[%d].task_type", i), "Unknown task type %q", c.TaskType)
		}
		if strings.TrimSpace(c.Engine) == "" {
			v.Add(fmt.Sprintf("capabilities[%d].engine", i), "Engine name is required")
		}
		for j := range c.Languages {
			req.Capabilities[i].Languages[j] = strings.ToLower(c.Languages[j])
		}
		for j := range c.Tags {
			req.Capabilities[i].Tags[j] = strings.ToLower(c.Tags[j])
		}
		if c.Concurrency < 1 {
			req.Capabilities[i].Concurrency = 1
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	caps, _ := json.Marshal(req.Capabilities)
	if _, err := s.pool.Exec(ctx, `UPDATE workers SET protocol_version=$2, version=$3, host=$4, capabilities=$5, last_seen_at=now() WHERE id=$1`,
		p.WorkerID, proto, truncate(req.Worker.Version, 64), truncate(req.Worker.Host, 128), caps); err != nil {
		return nil, err
	}
	cfg := loadSettings(ctx, s.settings)
	return &HelloResponse{WorkerID: p.WorkerID, Protocol: proto, HeartbeatInterval: max(10, cfg.LeaseSeconds/4),
		LeaseTTL: cfg.LeaseSeconds, MaxLeaseWait: 30, ServerVersion: ServerVersion}, nil
}

// truncate cuts s to at most n bytes without splitting a UTF-8 character.
func truncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

type LeaseRequest struct {
	Capacity    int      `json:"capacity"`
	WaitSeconds int      `json:"wait_seconds"`
	Types       []string `json:"types,omitempty"` // only lease these task types (default: all advertised)
}

type TaskInput struct {
	URL    string `json:"url"`
	Mime   string `json:"mime"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type LeasedTask struct {
	TaskID         uuid.UUID      `json:"task_id"`
	LeaseID        uuid.UUID      `json:"lease_id"`
	Type           string         `json:"type"`
	Engine         string         `json:"engine"` // the capability this task was matched to
	Attempt        int            `json:"attempt"`
	LeaseExpiresAt time.Time      `json:"lease_expires_at"`
	DocumentID     uuid.UUID      `json:"document_id"`
	PageRange      []int          `json:"page_range"` // [from, to] 1-based inclusive; null = whole file
	Hints          map[string]any `json:"hints"`
	Input          TaskInput      `json:"input"`
	OCRResultURL   string         `json:"ocr_result_url,omitempty"` // archive tasks
	CompleteURL    string         `json:"complete_url"`
	MaxResultBytes int64          `json:"max_result_bytes"`
}

const maxResultBytes = 512 << 20

// Lease hands out up to capacity tasks matching the worker's capabilities, waiting up
// to WaitSeconds (long poll) when none are available.
func (s *Service) Lease(ctx context.Context, p *auth.Principal, req LeaseRequest) ([]LeasedTask, error) {
	if p == nil || p.Kind != auth.KindWorker {
		return nil, apperr.Unauthorized("Worker token required")
	}
	wait := time.Duration(min(max(req.WaitSeconds, 0), 30)) * time.Second
	deadline := time.Now().Add(wait)
	for {
		ch := s.waitCh()
		tasks, err := s.tryLease(ctx, p.WorkerID, req.Capacity, req.Types)
		if err != nil || len(tasks) > 0 || !time.Now().Before(deadline) {
			return tasks, err
		}
		timer := time.NewTimer(min(time.Until(deadline), 5*time.Second)) // periodic re-check covers fallback timers
		select {
		case <-ctx.Done():
			timer.Stop()
			return []LeasedTask{}, nil
		case <-ch:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (s *Service) tryLease(ctx context.Context, workerID uuid.UUID, capacity int, types []string) ([]LeasedTask, error) {
	var caps []Capability
	var raw []byte
	var enabled bool
	if err := s.pool.QueryRow(ctx, `UPDATE workers SET last_seen_at=now(), offline_notified=false WHERE id=$1 RETURNING capabilities, enabled`,
		workerID).Scan(&raw, &enabled); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, apperr.Forbidden("This worker is disabled in Docveta")
	}
	_ = json.Unmarshal(raw, &caps)
	if len(caps) == 0 {
		return nil, apperr.Conflict("hello_required", "Send /worker/v1/hello with capabilities first")
	}
	cfg := loadSettings(ctx, s.settings)

	var active int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM processing_tasks WHERE worker_id=$1 AND status='leased'`, workerID).Scan(&active); err != nil {
		return nil, err
	}
	out := []LeasedTask{}
	for _, c := range caps {
		if len(types) > 0 && !slices.Contains(types, c.TaskType) {
			continue
		}
		limit := min(max(capacity, 1), c.Concurrency) - countType(out, c.TaskType)
		if limit <= 0 || active >= totalConcurrency(caps) {
			continue
		}
		mimes := c.InputMime
		if len(mimes) == 0 {
			mimes = []string{"*/*"}
		}
		langs := c.Languages
		if langs == nil {
			langs = []string{}
		}
		tags := c.Tags
		if tags == nil {
			tags = []string{}
		}
		maxPages := c.MaxPagesPerTask
		if maxPages <= 0 {
			maxPages = 1 << 30
		}
		var leased []LeasedTask
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `WITH c AS (
					SELECT t.id FROM processing_tasks t JOIN documents d ON d.id=t.document_id
					WHERE t.status='queued' AND t.not_before <= now() AND t.type=$1 AND d.deleted_at IS NULL
					  AND (t.mime_type = ANY($2) OR split_part(t.mime_type,'/',1) || '/*' = ANY($2) OR '*/*' = ANY($2))
					  AND NOT ($3 = ANY(t.excluded_engines))
					  AND ((t.required_tags <@ $4 AND t.required_languages <@ $5) OR (t.fallback_at IS NOT NULL AND t.fallback_at <= now()))
					  AND (t.page_to IS NULL OR t.page_to - t.page_from + 1 <= $6)
					ORDER BY t.priority, t.created_at
					LIMIT $7 FOR UPDATE OF t SKIP LOCKED)
				UPDATE processing_tasks t SET status='leased', lease_id=gen_random_uuid(), worker_id=$8,
					lease_expires_at=now() + make_interval(secs => $9), attempt=t.attempt+1, updated_at=now()
				FROM c WHERE t.id=c.id
				RETURNING t.id, t.lease_id, t.type, t.attempt, t.lease_expires_at, t.document_id, t.page_from, t.page_to,
					t.mime_type, t.required_languages, t.payload`,
				c.TaskType, mimes, c.Engine, tags, langs, maxPages, limit, workerID, cfg.LeaseSeconds)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var lt LeasedTask
				var from, to *int
				var mime string
				var reqLangs []string
				var payload []byte
				if err := rows.Scan(&lt.TaskID, &lt.LeaseID, &lt.Type, &lt.Attempt, &lt.LeaseExpiresAt, &lt.DocumentID, &from, &to,
					&mime, &reqLangs, &payload); err != nil {
					return err
				}
				lt.Engine = c.Engine
				if from != nil && to != nil {
					lt.PageRange = []int{*from, *to}
				}
				hintLangs := reqLangs
				if len(c.Languages) > 0 && !subset(reqLangs, c.Languages) {
					// Fallback routing to a worker without the requested language.
					hintLangs = append(slices.Clone(reqLangs), c.Languages[0])
				}
				lt.Hints = map[string]any{"languages": hintLangs, "dpi": 300, "deskew": true, "detect_rotation": true}
				lt.Input = TaskInput{URL: fmt.Sprintf("/worker/v1/tasks/%s/input", lt.TaskID), Mime: mime}
				if lt.Type == "archive" {
					lt.OCRResultURL = fmt.Sprintf("/worker/v1/tasks/%s/ocr-result", lt.TaskID)
				}
				lt.CompleteURL = fmt.Sprintf("/worker/v1/tasks/%s/complete", lt.TaskID)
				lt.MaxResultBytes = maxResultBytes
				leased = append(leased, lt)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			for i := range leased {
				var sha []byte
				if err := tx.QueryRow(ctx, `SELECT f.sha256, f.size_bytes, f.mime_type FROM document_files f JOIN documents d ON d.id=f.document_id
					WHERE f.document_id=$1 AND f.kind IN ('derived','original') AND f.version_no=d.current_version
					ORDER BY (f.kind='derived') DESC LIMIT 1`, leased[i].DocumentID).
					Scan(&sha, &leased[i].Input.Size, &leased[i].Input.Mime); err != nil {
					return err
				}
				leased[i].Input.SHA256 = fmt.Sprintf("%x", sha)
				if leased[i].Type == "ocr" {
					if _, err := tx.Exec(ctx, `UPDATE documents SET processing_stage='ocr' WHERE id=$1 AND status='processing'`, leased[i].DocumentID); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		active += len(leased)
		out = append(out, leased...)
	}
	return out, nil
}

func subset(a, b []string) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

func countType(ts []LeasedTask, typ string) int {
	n := 0
	for _, t := range ts {
		if t.Type == typ {
			n++
		}
	}
	return n
}

func totalConcurrency(caps []Capability) int {
	n := 0
	for _, c := range caps {
		n += max(c.Concurrency, 1)
	}
	return n
}

// leasedTask loads a task and verifies the caller holds its current lease.
func (s *Service) leasedTask(ctx context.Context, q db.Querier, p *auth.Principal, taskID, leaseID uuid.UUID, lock bool) (*taskState, error) {
	if p == nil || p.Kind != auth.KindWorker {
		return nil, apperr.Unauthorized("Worker token required")
	}
	sql := `SELECT id, document_id, ocr_run_id, type, status, lease_id, worker_id, page_from, page_to, attempt, max_attempts, payload
		FROM processing_tasks WHERE id=$1`
	if lock {
		sql += " FOR UPDATE"
	}
	var t taskState
	var payload []byte
	err := q.QueryRow(ctx, sql, taskID).Scan(&t.ID, &t.DocumentID, &t.RunID, &t.Type, &t.Status, &t.LeaseID, &t.WorkerID,
		&t.PageFrom, &t.PageTo, &t.Attempt, &t.MaxAttempts, &payload)
	if db.IsNoRows(err) {
		return nil, apperr.NotFound("Task")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(payload, &t.Payload)
	if t.Status != "leased" || t.LeaseID == nil || *t.LeaseID != leaseID || t.WorkerID == nil || *t.WorkerID != p.WorkerID {
		return nil, &apperr.Error{Kind: apperr.KindConflict, Code: "lease_lost",
			Msg: "This lease is no longer valid (expired, cancelled or re-assigned). Abort the task."}
	}
	return &t, nil
}

type taskState struct {
	ID          uuid.UUID
	DocumentID  uuid.UUID
	RunID       *uuid.UUID
	Type        string
	Status      string
	LeaseID     *uuid.UUID
	WorkerID    *uuid.UUID
	PageFrom    *int
	PageTo      *int
	Attempt     int
	MaxAttempts int
	Payload     map[string]any
}

type HeartbeatRequest struct {
	LeaseID  uuid.UUID      `json:"lease_id"`
	Progress map[string]any `json:"progress"`
}

func (s *Service) Heartbeat(ctx context.Context, p *auth.Principal, taskID uuid.UUID, req HeartbeatRequest) (time.Time, error) {
	if _, err := s.leasedTask(ctx, s.pool, p, taskID, req.LeaseID, false); err != nil {
		return time.Time{}, err
	}
	cfg := loadSettings(ctx, s.settings)
	progress, _ := json.Marshal(req.Progress)
	var exp time.Time
	err := s.pool.QueryRow(ctx, `UPDATE processing_tasks SET lease_expires_at=now() + make_interval(secs => $3), progress=$4, updated_at=now()
		WHERE id=$1 AND lease_id=$2 AND status='leased' RETURNING lease_expires_at`, taskID, req.LeaseID, cfg.LeaseSeconds, progress).Scan(&exp)
	if db.IsNoRows(err) {
		return time.Time{}, &apperr.Error{Kind: apperr.KindConflict, Code: "lease_lost", Msg: "Lease lost"}
	}
	_, _ = s.pool.Exec(ctx, `UPDATE workers SET last_seen_at=now() WHERE id=$1`, p.WorkerID)
	return exp, err
}

// OpenTaskInput streams the original file of a leased task.
func (s *Service) OpenTaskInput(ctx context.Context, p *auth.Principal, taskID, leaseID uuid.UUID) (io.ReadSeekCloser, int64, string, error) {
	t, err := s.leasedTask(ctx, s.pool, p, taskID, leaseID, false)
	if err != nil {
		return nil, 0, "", err
	}
	var key, mime string
	// The working copy (HEIC converted to JPEG, Office converted to PDF) wins over the original.
	if err := s.pool.QueryRow(ctx, `SELECT f.blob_key, f.mime_type FROM document_files f JOIN documents d ON d.id=f.document_id
		WHERE f.document_id=$1 AND f.kind IN ('derived','original') AND f.version_no=d.current_version
		ORDER BY (f.kind='derived') DESC LIMIT 1`, t.DocumentID).Scan(&key, &mime); err != nil {
		return nil, 0, "", err
	}
	r, size, err := s.store.Open(ctx, key)
	return r, size, mime, err
}

// OpenTaskOCRResult streams the merged OCR result for archive tasks.
func (s *Service) OpenTaskOCRResult(ctx context.Context, p *auth.Principal, taskID, leaseID uuid.UUID) (io.ReadSeekCloser, int64, error) {
	t, err := s.leasedTask(ctx, s.pool, p, taskID, leaseID, false)
	if err != nil {
		return nil, 0, err
	}
	key, _ := t.Payload["ocr_result_key"].(string)
	if t.Type != "archive" || key == "" {
		return nil, 0, apperr.NotFound("OCR result")
	}
	return s.store.Open(ctx, key)
}

type CompleteInput struct {
	LeaseID uuid.UUID
	Metrics map[string]any
	Result  io.Reader // canonical OCR JSON (ocr tasks)
	Archive io.Reader // searchable PDF (archive tasks)
}

// Complete stores a task's result. Results from stale leases are rejected so a slow
// "zombie" worker can never overwrite a re-assigned task.
func (s *Service) Complete(ctx context.Context, p *auth.Principal, taskID uuid.UUID, in CompleteInput) error {
	t, err := s.leasedTask(ctx, s.pool, p, taskID, in.LeaseID, false)
	if err != nil {
		return err
	}
	var resultKey string
	var archiveKey string
	var archiveSum []byte
	var archiveSize int64
	switch t.Type {
	case "ocr":
		if in.Result == nil {
			return apperr.Invalid("result", "OCR tasks must upload a result file")
		}
		raw, err := io.ReadAll(io.LimitReader(in.Result, maxResultBytes+1))
		if err != nil {
			return err
		}
		if len(raw) > maxResultBytes {
			return apperr.Invalid("result", "Result is too large")
		}
		from, to := 0, 0
		if t.PageFrom != nil && t.PageTo != nil {
			from, to = *t.PageFrom, *t.PageTo
		}
		if _, err := ParseOCRResult(raw, from, to); err != nil {
			// A malformed result is an engine bug: count it as a non-retryable failure.
			return s.Fail(ctx, p, taskID, FailRequest{LeaseID: in.LeaseID, Code: "engine_error", Message: "invalid result: " + err.Error(), Retryable: false})
		}
		if resultKey, _, err = storage.PutBytes(s.store, raw); err != nil {
			return err
		}
	case "archive":
		if in.Archive == nil {
			return apperr.Invalid("archive", "Archive tasks must upload a PDF")
		}
		st, err := s.store.Stage()
		if err != nil {
			return err
		}
		n, err := io.Copy(st, io.LimitReader(in.Archive, maxResultBytes*4+1))
		if err != nil {
			st.Discard()
			return err
		}
		head := make([]byte, 5)
		if f, err := openHead(st.Path(), head); err != nil || string(f) != "%PDF-" {
			st.Discard()
			return s.Fail(ctx, p, taskID, FailRequest{LeaseID: in.LeaseID, Code: "engine_error", Message: "archive is not a PDF", Retryable: false})
		}
		archiveSum = st.SHA256()
		archiveSize = n
		if archiveKey, err = st.Commit(); err != nil {
			return err
		}
	default:
		return apperr.Invalid("type", "Unsupported task type")
	}
	metrics, _ := json.Marshal(in.Metrics)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.leasedTask(ctx, tx, p, taskID, in.LeaseID, true); err != nil {
			return err // lease lost while uploading
		}
		var rk any
		if resultKey != "" {
			rk = resultKey
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='done', result_blob_key=$2, metrics=$3, lease_id=NULL,
			lease_expires_at=NULL, finished_at=now(), updated_at=now(), last_error='' WHERE id=$1`, taskID, rk, metrics); err != nil {
			return err
		}
		if t.Type == "archive" {
			var version int
			var deleted bool
			if err := tx.QueryRow(ctx, `SELECT current_version, deleted_at IS NOT NULL FROM documents WHERE id=$1`, t.DocumentID).Scan(&version, &deleted); err != nil {
				return err
			}
			if deleted {
				return nil
			}
			if _, err := tx.Exec(ctx, `INSERT INTO document_files (id, document_id, kind, version_no, blob_key, sha256, mime_type, size_bytes)
				VALUES ($1,$2,'archive',$3,$4,$5,'application/pdf',$6)
				ON CONFLICT (document_id, kind, version_no) DO UPDATE SET blob_key=excluded.blob_key, sha256=excluded.sha256,
					size_bytes=excluded.size_bytes, created_at=now()`,
				uuid.Must(uuid.NewV7()), t.DocumentID, version, archiveKey, archiveSum, archiveSize); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE documents SET version=version+1 WHERE id=$1`, t.DocumentID); err != nil {
				return err
			}
			return documents.RecordEvent(ctx, tx, t.DocumentID, "archive_created", nil)
		}
		if t.RunID != nil {
			if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET pages_done = pages_done + coalesce($2::int, 1) WHERE id=$1`, *t.RunID, pageSpan(t)); err != nil {
				return err
			}
			return s.maybeFinalizeTx(ctx, tx, *t.RunID)
		}
		return nil
	})
}

// openHead reads the first len(buf) bytes of a file.
func openHead(path string, buf []byte) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := io.ReadFull(f, buf)
	return buf[:n], err
}

func pageSpan(t *taskState) any {
	if t.PageFrom != nil && t.PageTo != nil {
		return *t.PageTo - *t.PageFrom + 1
	}
	return nil
}

type FailRequest struct {
	LeaseID   uuid.UUID `json:"lease_id"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// Fail records a task failure. Retryable failures back off and retry; non-retryable
// ones exclude this engine and fall back to another capable engine if any exists.
func (s *Service) Fail(ctx context.Context, p *auth.Principal, taskID uuid.UUID, req FailRequest) error {
	msg := truncate(strings.TrimSpace(req.Code+": "+req.Message), 500)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		t, err := s.leasedTask(ctx, tx, p, taskID, req.LeaseID, true)
		if err != nil {
			return err
		}
		var engine string
		var caps []byte
		if err := tx.QueryRow(ctx, `SELECT capabilities FROM workers WHERE id=$1`, p.WorkerID).Scan(&caps); err == nil {
			var cs []Capability
			_ = json.Unmarshal(caps, &cs)
			for _, c := range cs {
				if c.TaskType == t.Type {
					engine = c.Engine
					break
				}
			}
		}
		status := "queued"
		notBefore := time.Now().Add(backoff(t.Attempt))
		excludeEngine := ""
		if !req.Retryable || t.Attempt >= t.MaxAttempts {
			// Is there another engine that could try?
			var other bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workers w, jsonb_array_elements(w.capabilities) c
				WHERE w.enabled AND w.last_seen_at > now() - interval '1 day' AND c->>'task_type'=$1 AND c->>'engine' <> $2
				  AND NOT (c->>'engine' = ANY((SELECT excluded_engines FROM processing_tasks WHERE id=$3))))`,
				t.Type, engine, t.ID).Scan(&other); err != nil {
				return err
			}
			if other && engine != "" {
				excludeEngine = engine
				notBefore = time.Now()
			} else {
				status = "failed"
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE processing_tasks SET status=$2, last_error=$3, not_before=$4, lease_id=NULL, worker_id=NULL,
			lease_expires_at=NULL, updated_at=now(),
			excluded_engines = CASE WHEN $5 <> '' THEN array_append(excluded_engines, $5) ELSE excluded_engines END,
			fallback_at = CASE WHEN $5 <> '' THEN now() ELSE fallback_at END,
			attempt = CASE WHEN $5 <> '' THEN 0 ELSE attempt END,
			finished_at = CASE WHEN $2='failed' THEN now() ELSE NULL END
			WHERE id=$1`, t.ID, status, msg, notBefore, excludeEngine); err != nil {
			return err
		}
		if status == "failed" && t.RunID != nil && t.Type == "ocr" {
			return s.maybeFinalizeTx(ctx, tx, *t.RunID)
		}
		if status == "queued" {
			defer s.signal()
		}
		return nil
	})
}

// Release returns a task to the queue without counting an attempt (graceful shutdown).
func (s *Service) Release(ctx context.Context, p *auth.Principal, taskID, leaseID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.leasedTask(ctx, tx, p, taskID, leaseID, true); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE processing_tasks SET status='queued', lease_id=NULL, worker_id=NULL, lease_expires_at=NULL,
			attempt=greatest(attempt-1,0), updated_at=now() WHERE id=$1`, taskID)
		if err == nil {
			defer s.signal()
		}
		return err
	})
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<min(attempt, 8)) * 15 * time.Second
	return min(d, time.Hour)
}

// ---------------------------------------------------------------------------
// Lease reaper (periodic)
// ---------------------------------------------------------------------------

type LeaseReaperWorker struct {
	river.WorkerDefaults[jobs.LeaseReaperArgs]
	S *Service
}

func (w *LeaseReaperWorker) Work(ctx context.Context, _ *river.Job[jobs.LeaseReaperArgs]) error {
	return w.S.ReapLeases(ctx)
}

// ReapLeases re-queues tasks whose worker stopped heartbeating.
func (s *Service) ReapLeases(ctx context.Context) error {
	var requeued bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE processing_tasks SET
				status = CASE WHEN attempt >= max_attempts THEN 'failed' ELSE 'queued' END,
				last_error = 'worker stopped responding (lease expired)',
				lease_id=NULL, worker_id=NULL, lease_expires_at=NULL, updated_at=now(),
				not_before = now() + make_interval(secs => 15 * attempt),
				finished_at = CASE WHEN attempt >= max_attempts THEN now() ELSE NULL END
			WHERE id IN (SELECT id FROM processing_tasks WHERE status='leased' AND lease_expires_at < now() FOR UPDATE SKIP LOCKED)
			RETURNING status, ocr_run_id, type`)
		if err != nil {
			return err
		}
		var runs []uuid.UUID
		for rows.Next() {
			var st, typ string
			var run *uuid.UUID
			if err := rows.Scan(&st, &run, &typ); err != nil {
				rows.Close()
				return err
			}
			if st == "failed" && run != nil && typ == "ocr" {
				runs = append(runs, *run)
			}
			if st == "queued" {
				requeued = true
			}
		}
		rows.Close()
		for _, r := range runs {
			if err := s.maybeFinalizeTx(ctx, tx, r); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if requeued {
		s.signal()
	}
	return err
}

// ---------------------------------------------------------------------------
// Admin task views
// ---------------------------------------------------------------------------

type TaskView struct {
	ID         uuid.UUID  `json:"id"`
	DocumentID uuid.UUID  `json:"document_id"`
	Title      string     `json:"document_title"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	PageFrom   *int       `json:"page_from"`
	PageTo     *int       `json:"page_to"`
	Priority   int        `json:"priority"`
	Attempt    int        `json:"attempt"`
	Worker     *string    `json:"worker"`
	LastError  string     `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type QueueStats struct {
	Queued       int `json:"queued"`
	Leased       int `json:"leased"`
	Failed24h    int `json:"failed_24h"`
	Done24h      int `json:"done_24h"`
	PagesDone24h int `json:"pages_done_24h"`
}

// taskOrder is how the task list is sorted: running work first, then waiting, failed, done, cancelled.
var taskOrder = []string{"leased", "queued", "failed", "done", "cancelled"}

// Tasks lists processing tasks a page at a time (newest first within each status) with the
// queue's numbers. cursor continues after the previous page; the returned one is empty at
// the end. Each status is read from its own index, so this stays quick with millions of tasks.
func (s *Service) Tasks(ctx context.Context, p *auth.Principal, status, cursor string, limit int) ([]TaskView, *QueueStats, string, error) {
	if !p.Admin() {
		return nil, nil, "", apperr.Forbidden("")
	}
	var st QueueStats
	if err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM processing_tasks WHERE status='queued'),
		(SELECT count(*) FROM processing_tasks WHERE status='leased'),
		(SELECT count(*) FROM processing_tasks WHERE status='failed' AND finished_at > now()-interval '1 day'),
		count(*), coalesce(sum(coalesce(page_to-page_from+1,1)),0)
		FROM processing_tasks WHERE status='done' AND finished_at > now()-interval '1 day' AND type='ocr'`).Scan(&st.Queued, &st.Leased, &st.Failed24h, &st.Done24h, &st.PagesDone24h); err != nil {
		return nil, nil, "", err
	}
	statuses := taskOrder
	if status != "" {
		if !slices.Contains(taskOrder, status) {
			return []TaskView{}, &st, "", nil // nothing has that status ("none": just the numbers)
		}
		statuses = []string{status}
	}
	// The cursor is "status|updated_at|id" of the last task shown.
	var after struct {
		status string
		at     time.Time
		id     uuid.UUID
	}
	if cursor != "" {
		parts := strings.Split(cursor, "|")
		var err1, err2 error
		if len(parts) == 3 {
			after.status = parts[0]
			after.at, err1 = time.Parse(time.RFC3339Nano, parts[1])
			after.id, err2 = uuid.Parse(parts[2])
		}
		if len(parts) != 3 || err1 != nil || err2 != nil || !slices.Contains(statuses, after.status) {
			return nil, nil, "", apperr.Invalid("cursor", "Invalid cursor")
		}
		statuses = statuses[slices.Index(statuses, after.status):]
	}
	out := []TaskView{}
	var lastAt time.Time
	for _, stt := range statuses {
		// Only the status the cursor stopped in continues after it; the ones below start at the top.
		cond, args := "", []any{stt, limit - len(out) + 1}
		if after.status == stt {
			cond, args = " AND (t.updated_at, t.id) < ($3, $4)", append(args, after.at, after.id)
		}
		rows, err := s.pool.Query(ctx, `SELECT t.id, t.document_id, d.title, t.type, t.status, t.page_from, t.page_to, t.priority, t.attempt,
			w.name, t.last_error, t.created_at, t.finished_at, t.updated_at
			FROM processing_tasks t JOIN documents d ON d.id=t.document_id LEFT JOIN workers w ON w.id=t.worker_id
			WHERE t.status=$1 AND t.type <> 'embedded'`+cond+`
			ORDER BY t.updated_at DESC, t.id DESC LIMIT $2`, args...)
		if err != nil {
			return nil, nil, "", err
		}
		var ats []time.Time
		page, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TaskView, error) {
			var t TaskView
			var at time.Time
			err := r.Scan(&t.ID, &t.DocumentID, &t.Title, &t.Type, &t.Status, &t.PageFrom, &t.PageTo, &t.Priority, &t.Attempt, &t.Worker,
				&t.LastError, &t.CreatedAt, &t.FinishedAt, &at)
			ats = append(ats, at)
			return t, err
		})
		if err != nil {
			return nil, nil, "", err
		}
		for i, t := range page {
			if len(out) == limit { // one more exists: there is a next page
				last := out[len(out)-1]
				return out, &st, last.Status + "|" + lastAt.Format(time.RFC3339Nano) + "|" + last.ID.String(), nil
			}
			out = append(out, t)
			lastAt = ats[i]
		}
	}
	return out, &st, "", nil
}

// RetryTask re-queues a failed task (admin).
func (s *Service) RetryTask(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var runID *uuid.UUID
		var docID uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE processing_tasks SET status='queued', attempt=0, excluded_engines='{}', not_before=now(),
			fallback_at=now(), last_error='', finished_at=NULL, updated_at=now() WHERE id=$1 AND status='failed'
			RETURNING ocr_run_id, document_id`, id).Scan(&runID, &docID)
		if db.IsNoRows(err) {
			return apperr.Conflict("not_failed", "Only failed tasks can be retried")
		}
		if err != nil {
			return err
		}
		if runID != nil {
			// Re-open the run if it was already finalized as failed.
			if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET status='running', finished_at=NULL WHERE id=$1 AND status='failed'`, *runID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE documents SET status='processing', processing_stage='awaiting_ocr' WHERE id=$1 AND status='failed'`, docID)
		return err
	})
	if err == nil {
		s.signal()
	}
	return err
}

// RetryFailed re-queues every failed task (admin), after the cause (a worker that was down,
// a missing language) is fixed. It returns how many were queued again.
func (s *Service) RetryFailed(ctx context.Context, p *auth.Principal) (int, error) {
	if !p.Admin() {
		return 0, apperr.Forbidden("")
	}
	n := 0
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE processing_tasks SET status='queued', attempt=0, excluded_engines='{}', not_before=now(),
			fallback_at=now(), last_error='', finished_at=NULL, updated_at=now() WHERE status='failed'
			RETURNING ocr_run_id, document_id`)
		if err != nil {
			return err
		}
		var runs, docs []uuid.UUID
		for rows.Next() {
			var run *uuid.UUID
			var doc uuid.UUID
			if err := rows.Scan(&run, &doc); err != nil {
				rows.Close()
				return err
			}
			if run != nil {
				runs = append(runs, *run)
			}
			docs = append(docs, doc)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		n = len(docs)
		if _, err := tx.Exec(ctx, `UPDATE ocr_runs SET status='running', finished_at=NULL WHERE id = ANY($1) AND status='failed'`, runs); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE documents SET status='processing', processing_stage='awaiting_ocr' WHERE id = ANY($1) AND status='failed'`, docs)
		return err
	})
	if err == nil && n > 0 {
		s.signal()
	}
	return n, err
}

// OCRAvailable reports whether an enabled worker that can read text has been seen
// recently. Without one, scans and photos stay unsearchable, so the UI says so.
func (s *Service) OCRAvailable(ctx context.Context) bool {
	var ok bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workers WHERE enabled AND last_seen_at > now() - interval '10 minutes'
		AND capabilities @> '[{"task_type":"ocr"}]')`).Scan(&ok)
	return ok
}
