package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/settings"
)

// EventTypes users can subscribe channels to (shown in Settings → Notifications).
var EventTypes = []string{
	"document.processed", "document.failed", "note.mention", "reminder.due",
	"security.new_login", "security.token_created", "worker.offline", "worker.online", "storage.low",
}

type Service struct {
	pool     *pgxpool.Pool
	keys     *crypto.Keys
	settings *settings.Store
	cfg      *config.Config
	log      *slog.Logger
	Hub      *Hub
}

func NewService(pool *pgxpool.Pool, keys *crypto.Keys, st *settings.Store, cfg *config.Config, log *slog.Logger) *Service {
	httpClient = newHTTPClient(cfg.AllowLocalTargets)
	return &Service{pool: pool, keys: keys, settings: st, cfg: cfg, log: log, Hub: NewHub()}
}

// ---------------------------------------------------------------------------
// In-app notifications
// ---------------------------------------------------------------------------

type Notification struct {
	ID        uuid.UUID  `json:"id"`
	EventType string     `json:"event_type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	Severity  string     `json:"severity"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func (s *Service) List(ctx context.Context, p *auth.Principal, unreadOnly bool, limit int) ([]Notification, int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, event_type, title, body, link, severity, read_at, created_at FROM notifications
		WHERE user_id=$1 AND (NOT $2 OR read_at IS NULL) ORDER BY created_at DESC LIMIT $3`, p.UserID, unreadOnly, limit)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		err := r.Scan(&n.ID, &n.EventType, &n.Title, &n.Body, &n.Link, &n.Severity, &n.ReadAt, &n.CreatedAt)
		return n, err
	})
	if err != nil {
		return nil, 0, err
	}
	var unread int
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, p.UserID).Scan(&unread)
	return list, unread, err
}

// MarkRead marks notifications read; ids == nil marks all.
func (s *Service) MarkRead(ctx context.Context, p *auth.Principal, ids []uuid.UUID) error {
	if ids == nil {
		_, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, p.UserID)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND id = ANY($2) AND read_at IS NULL`, p.UserID, ids)
	return err
}

// ---------------------------------------------------------------------------
// Channels
// ---------------------------------------------------------------------------

type Channel struct {
	ID           uuid.UUID      `json:"id"`
	System       bool           `json:"system"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	Config       map[string]any `json:"config"` // secrets redacted
	Events       []string       `json:"events"`
	Enabled      bool           `json:"enabled"`
	FailureCount int            `json:"failure_count"`
	LastError    string         `json:"last_error"`
}

type ChannelInput struct {
	Name    *string         `json:"name"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Events  []string        `json:"events"`
	Enabled *bool           `json:"enabled"`
	System  bool            `json:"system"` // admin-only: channel for instance alerts
}

var secretFields = map[string]bool{"token": true, "secret": true}

func redact(cfg map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range cfg {
		if secretFields[k] {
			if s, _ := v.(string); s != "" {
				out[k] = "••••••"
			} else {
				out[k] = ""
			}
			continue
		}
		out[k] = v
	}
	return out
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// validateConfig checks a channel config; old supplies existing secrets when the client
// sends back the redacted placeholder.
func validateConfig(typ string, raw json.RawMessage, old map[string]any) (map[string]any, error) {
	var cfg map[string]any
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, apperr.Invalid("config", "Invalid configuration")
	}
	for k := range secretFields {
		if v, _ := cfg[k].(string); v == "••••••" && old != nil {
			cfg[k] = old[k]
		}
	}
	str := func(k string) string { s, _ := cfg[k].(string); return strings.TrimSpace(s) }
	var v apperr.Validation
	switch typ {
	case "gotify":
		if !validURL(str("url")) {
			v.Add("config.url", "Enter your Gotify server URL, e.g. https://gotify.example.com")
		}
		if str("token") == "" {
			v.Add("config.token", "Enter the application token from Gotify")
		}
		cfg = map[string]any{"url": strings.TrimRight(str("url"), "/"), "token": str("token")}
	case "ntfy":
		server := str("server")
		if server != "" && !validURL(server) {
			v.Add("config.server", "Enter a valid URL")
		}
		if str("topic") == "" {
			v.Add("config.topic", "Enter a topic")
		}
		cfg = map[string]any{"server": server, "topic": str("topic"), "token": str("token")}
	case "webhook":
		if !validURL(str("url")) {
			v.Add("config.url", "Enter a valid URL")
		}
		secret := str("secret")
		if secret == "" {
			t, _ := crypto.NewToken("whsec")
			secret = t
		}
		cfg = map[string]any{"url": str("url"), "secret": secret}
	case "email":
		to := str("to")
		if _, err := mail.ParseAddress(to); to != "" && err != nil {
			v.Add("config.to", "Enter a valid email address")
		}
		cfg = map[string]any{"to": to}
	default:
		v.Add("type", "Unknown channel type")
	}
	return cfg, v.Err()
}

func (s *Service) scanChannel(row pgx.Row) (*Channel, map[string]any, error) {
	var c Channel
	var owner *uuid.UUID
	var sealed []byte
	err := row.Scan(&c.ID, &owner, &c.Name, &c.Type, &sealed, &c.Events, &c.Enabled, &c.FailureCount, &c.LastError)
	if err != nil {
		return nil, nil, err
	}
	c.System = owner == nil
	var cfg map[string]any
	if plain, err := s.keys.Decrypt(sealed); err == nil {
		_ = json.Unmarshal(plain, &cfg)
	}
	c.Config = redact(cfg)
	if c.Events == nil {
		c.Events = []string{}
	}
	return &c, cfg, nil
}

const channelCols = `id, owner_id, name, type, config_encrypted, events, enabled, failure_count, last_error`

func (s *Service) Channels(ctx context.Context, p *auth.Principal) ([]*Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels
		WHERE owner_id=$1 OR (owner_id IS NULL AND $2) ORDER BY owner_id NULLS LAST, name`, p.UserID, p.Admin())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Channel{}
	for rows.Next() {
		c, _, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) loadChannel(ctx context.Context, p *auth.Principal, id uuid.UUID) (*Channel, map[string]any, error) {
	c, cfg, err := s.scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE id=$1
		AND (owner_id=$2 OR (owner_id IS NULL AND $3))`, id, p.UserID, p.Admin()))
	if db.IsNoRows(err) {
		return nil, nil, apperr.NotFound("Channel")
	}
	return c, cfg, err
}

func cleanEvents(evts []string) []string {
	out := []string{}
	for _, e := range evts {
		if slices.Contains(EventTypes, e) && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

func (s *Service) CreateChannel(ctx context.Context, p *auth.Principal, in ChannelInput) (*Channel, error) {
	if in.System && !p.Admin() {
		return nil, apperr.Forbidden("Only administrators can create system channels")
	}
	cfg, err := validateConfig(in.Type, in.Config, nil)
	if err != nil {
		return nil, err
	}
	name := in.Type
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = strings.TrimSpace(*in.Name)
	}
	var owner *uuid.UUID
	if !in.System {
		owner = &p.UserID
	}
	raw, _ := json.Marshal(cfg)
	id := uuid.Must(uuid.NewV7())
	if _, err := s.pool.Exec(ctx, `INSERT INTO notification_channels (id, owner_id, name, type, config_encrypted, events, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,coalesce($7,true))`, id, owner, name, in.Type, s.keys.Encrypt(raw), cleanEvents(in.Events), in.Enabled); err != nil {
		return nil, err
	}
	c, _, err := s.loadChannel(ctx, p, id)
	return c, err
}

func (s *Service) UpdateChannel(ctx context.Context, p *auth.Principal, id uuid.UUID, in ChannelInput) (*Channel, error) {
	c, old, err := s.loadChannel(ctx, p, id)
	if err != nil {
		return nil, err
	}
	sealed := []byte(nil)
	if len(in.Config) > 0 {
		cfg, err := validateConfig(c.Type, in.Config, old)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(cfg)
		sealed = s.keys.Encrypt(raw)
	}
	var events []string
	if in.Events != nil {
		events = cleanEvents(in.Events)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE notification_channels SET name=coalesce($2,name), config_encrypted=coalesce($3,config_encrypted),
		events=coalesce($4,events), enabled=coalesce($5,enabled), failure_count=CASE WHEN $5 THEN 0 ELSE failure_count END, updated_at=now()
		WHERE id=$1`, id, in.Name, sealed, events, in.Enabled); err != nil {
		return nil, err
	}
	c, _, err = s.loadChannel(ctx, p, id)
	return c, err
}

func (s *Service) DeleteChannel(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	if _, _, err := s.loadChannel(ctx, p, id); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id=$1`, id)
	return err
}

// TestChannel sends a test message synchronously and returns any delivery error.
func (s *Service) TestChannel(ctx context.Context, p *auth.Principal, id uuid.UUID) error {
	c, cfg, err := s.loadChannel(ctx, p, id)
	if err != nil {
		return err
	}
	msg := Message{Title: "Docveta test notification", Body: "If you can read this, \"" + c.Name + "\" is set up correctly.",
		URL: s.cfg.BaseURL.String(), Severity: "info", Event: jobs.Event{Type: "test", At: time.Now()}}
	if err := s.deliver(ctx, c.Type, cfg, p.UserID, msg); err != nil {
		return &apperr.Error{Kind: apperr.KindValidation, Code: "delivery_failed", Msg: "Sending failed: " + err.Error()}
	}
	return nil
}

func (s *Service) deliver(ctx context.Context, typ string, cfg map[string]any, owner uuid.UUID, m Message) error {
	raw, _ := json.Marshal(cfg)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch typ {
	case "gotify":
		var c GotifyConfig
		_ = json.Unmarshal(raw, &c)
		return sendGotify(ctx, c, m)
	case "ntfy":
		var c NtfyConfig
		_ = json.Unmarshal(raw, &c)
		return sendNtfy(ctx, c, m)
	case "webhook":
		var c WebhookConfig
		_ = json.Unmarshal(raw, &c)
		return sendWebhook(ctx, c, m)
	case "email":
		var c EmailConfig
		_ = json.Unmarshal(raw, &c)
		to := c.To
		if to == "" && owner != uuid.Nil {
			if err := s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, owner).Scan(&to); err != nil {
				return err
			}
		}
		if to == "" {
			return fmt.Errorf("no recipient address")
		}
		smtpCfg, pw, err := s.smtp(ctx)
		if err != nil {
			return err
		}
		return sendEmail(ctx, smtpCfg, pw, to, m)
	}
	return fmt.Errorf("unknown channel type %q", typ)
}

// ---------------------------------------------------------------------------
// SMTP settings
// ---------------------------------------------------------------------------

const smtpKey, smtpSecret = "smtp", "smtp.password"

func (s *Service) smtp(ctx context.Context) (SMTPConfig, string, error) {
	c := SMTPConfig{Port: 587, Security: "starttls"}
	if _, err := s.settings.Get(ctx, smtpKey, &c); err != nil {
		return c, "", err
	}
	pw, err := s.settings.GetSecret(ctx, smtpSecret)
	c.HasPassword = pw != ""
	c.Password = nil
	return c, pw, err
}

func (s *Service) SMTPSettings(ctx context.Context, p *auth.Principal) (SMTPConfig, error) {
	if !p.Admin() {
		return SMTPConfig{}, apperr.Forbidden("")
	}
	c, _, err := s.smtp(ctx)
	return c, err
}

func (s *Service) SetSMTPSettings(ctx context.Context, p *auth.Principal, c SMTPConfig) (SMTPConfig, error) {
	if !p.Admin() {
		return SMTPConfig{}, apperr.Forbidden("")
	}
	var v apperr.Validation
	if c.Enabled {
		if strings.TrimSpace(c.Host) == "" {
			v.Add("host", "Host is required")
		}
		if c.Port <= 0 || c.Port > 65535 {
			v.Add("port", "Invalid port")
		}
		if !strings.Contains(c.From, "@") && !strings.Contains(c.Username, "@") {
			v.Add("from", "Enter the sender address, e.g. Docveta <docveta@example.com>")
		}
	}
	switch c.Security {
	case "starttls", "tls", "none":
	default:
		v.Add("security", "Must be starttls, tls or none")
	}
	if err := v.Err(); err != nil {
		return SMTPConfig{}, err
	}
	if c.Password != nil {
		if err := s.settings.SetSecret(ctx, smtpSecret, *c.Password); err != nil {
			return SMTPConfig{}, err
		}
	}
	c.Password = nil
	if err := s.settings.Set(ctx, smtpKey, c, p.UserID); err != nil {
		return SMTPConfig{}, err
	}
	out, _, err := s.smtp(ctx)
	return out, err
}

// SendTestEmail sends a test email to the admin's address using the saved settings.
func (s *Service) SendTestEmail(ctx context.Context, p *auth.Principal) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	c, pw, err := s.smtp(ctx)
	if err != nil {
		return err
	}
	err = sendEmail(ctx, c, pw, p.Email, Message{Title: "Docveta test email", Body: "Email delivery from Docveta works.", URL: s.cfg.BaseURL.String()})
	if err != nil {
		return &apperr.Error{Kind: apperr.KindValidation, Code: "delivery_failed", Msg: "Sending failed: " + err.Error()}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Delivery (River worker)
// ---------------------------------------------------------------------------

type Worker struct {
	river.WorkerDefaults[jobs.NotifyArgs]
	S *Service
}

func (w *Worker) Work(ctx context.Context, job *river.Job[jobs.NotifyArgs]) error {
	return w.S.dispatch(ctx, job.Args.Event, job.ID)
}

// dispatch delivers an event. In-app rows use IDs derived from the job ID so a retried
// job never creates duplicates.
func (s *Service) dispatch(ctx context.Context, e jobs.Event, jobID int64) error {
	recipients := slices.Clone(e.Recipients)
	if e.ToAdmins {
		rows, err := s.pool.Query(ctx, `SELECT id FROM users WHERE is_admin AND status='active'`)
		if err != nil {
			return err
		}
		admins, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, a := range admins {
			if !slices.Contains(recipients, a) {
				recipients = append(recipients, a)
			}
		}
	}
	sev := e.Severity
	if sev == "" {
		sev = "info"
	}
	for _, uid := range recipients {
		n := Notification{ID: uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "docveta-notify/%d/%s", jobID, uid)),
			EventType: e.Type, Title: e.Title, Body: e.Body, Link: e.Link, Severity: sev, CreatedAt: time.Now()}
		tag, err := s.pool.Exec(ctx, `INSERT INTO notifications (id, user_id, event_type, title, body, link, severity)
			VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`, n.ID, uid, n.EventType, n.Title, n.Body, n.Link, n.Severity)
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				continue // user deleted meanwhile
			}
			return err
		}
		if tag.RowsAffected() > 0 {
			s.Hub.Publish(uid, "notification", n)
		}
	}

	// External channels: users' own channels for their events, plus system channels
	// for instance-level (admin) events. Failures are recorded, never retried here, so
	// one broken channel can't spam the others.
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels
		WHERE enabled AND ((owner_id = ANY($1)) OR (owner_id IS NULL AND $2)) AND (events = '{}' OR $3 = ANY(events))`,
		recipients, e.ToAdmins, e.Type)
	if err != nil {
		return err
	}
	type target struct {
		ch    *Channel
		cfg   map[string]any
		owner uuid.UUID
	}
	var targets []target
	for rows.Next() {
		var c Channel
		var owner *uuid.UUID
		var sealed []byte
		if err := rows.Scan(&c.ID, &owner, &c.Name, &c.Type, &sealed, &c.Events, &c.Enabled, &c.FailureCount, &c.LastError); err != nil {
			rows.Close()
			return err
		}
		plain, err := s.keys.Decrypt(sealed)
		if err != nil {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal(plain, &cfg)
		t := target{ch: &c, cfg: cfg}
		if owner != nil {
			t.owner = *owner
		}
		targets = append(targets, t)
	}
	rows.Close()
	link := ""
	if e.Link != "" {
		link = s.cfg.BaseURL.String() + e.Link
	}
	msg := Message{Title: e.Title, Body: e.Body, URL: link, Severity: sev, Event: e}
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			err := s.deliver(ctx, t.ch.Type, t.cfg, t.owner, msg)
			if err != nil {
				s.log.Warn("notification delivery failed", "channel", t.ch.ID, "type", t.ch.Type, "err", err)
				// Auto-disable after many consecutive failures; owners see the error in Settings.
				_, _ = s.pool.Exec(context.Background(), `UPDATE notification_channels SET failure_count=failure_count+1, last_error=$2,
					enabled = failure_count+1 < 20 WHERE id=$1`, t.ch.ID, truncate(err.Error(), 300))
				return
			}
			if t.ch.FailureCount > 0 {
				_, _ = s.pool.Exec(context.Background(), `UPDATE notification_channels SET failure_count=0, last_error='' WHERE id=$1`, t.ch.ID)
			}
		}(t)
	}
	wg.Wait()
	return nil
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

// ---------------------------------------------------------------------------
// SSE hub
// ---------------------------------------------------------------------------

type HubMessage struct {
	Event string
	Data  any
}

// Hub fans out live events to connected browser sessions (per user).
type Hub struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan HubMessage]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[uuid.UUID]map[chan HubMessage]struct{}{}} }

func (h *Hub) Subscribe(uid uuid.UUID) (chan HubMessage, func()) {
	ch := make(chan HubMessage, 16)
	h.mu.Lock()
	if h.subs[uid] == nil {
		h.subs[uid] = map[chan HubMessage]struct{}{}
	}
	h.subs[uid][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[uid], ch)
		if len(h.subs[uid]) == 0 {
			delete(h.subs, uid)
		}
		h.mu.Unlock()
	}
}

// Publish sends a message to all of a user's connections without blocking.
func (h *Hub) Publish(uid uuid.UUID, event string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[uid] {
		select {
		case ch <- HubMessage{Event: event, Data: data}:
		default: // slow client: drop; the UI refetches on reconnect
		}
	}
}
