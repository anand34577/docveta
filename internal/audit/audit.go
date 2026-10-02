// Package audit records security-relevant and administrative actions.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

type Log struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Log { return &Log{pool: pool, log: log} }

type Entry struct {
	ID         int64          `json:"id"`
	At         time.Time      `json:"at"`
	ActorID    *uuid.UUID     `json:"actor_id"`
	ActorName  string         `json:"actor_name"`
	ActorType  string         `json:"actor_type"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"user_agent"`
	Details    map[string]any `json:"details"`
}

// Record writes an audit entry. Failures are logged, never returned: auditing must not
// break the user's action.
func (l *Log) Record(ctx context.Context, q db.Querier, action, targetType, targetID string, details map[string]any) {
	p := auth.From(ctx)
	var actorID *uuid.UUID
	actorType := "anonymous"
	if p != nil {
		actorType = string(p.Kind)
		if p.UserID != uuid.Nil {
			id := p.UserID
			actorID = &id
		}
	}
	if details == nil {
		details = map[string]any{}
	}
	raw, _ := json.Marshal(details)
	if q == nil {
		q = l.pool
	}
	ua := ""
	if v, ok := ctx.Value(uaKey{}).(string); ok {
		ua = v
	}
	if _, err := q.Exec(ctx, `INSERT INTO audit_log (actor_id, actor_type, action, target_type, target_id, ip, user_agent, details)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, actorID, actorType, action, targetType, targetID, httpx.ClientIP(ctx), ua, raw); err != nil {
		l.log.Error("audit write failed", "err", err, "action", action)
	}
}

type uaKey struct{}

// WithUserAgent stores the user agent for audit entries.
func WithUserAgent(ctx context.Context, ua string) context.Context {
	if len(ua) > 300 {
		ua = ua[:300]
	}
	return context.WithValue(ctx, uaKey{}, ua)
}

// List returns recent entries (admin only), optionally filtered by action prefix.
func (l *Log) List(ctx context.Context, p *auth.Principal, action string, before int64, limit int) ([]Entry, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := l.pool.Query(ctx, `SELECT a.id, a.at, a.actor_id, coalesce(u.display_name,''), a.actor_type, a.action,
		a.target_type, a.target_id, a.ip, a.user_agent, a.details
		FROM audit_log a LEFT JOIN users u ON u.id=a.actor_id
		WHERE a.id < $1 AND ($2='' OR a.action LIKE $2 || '%')
		ORDER BY a.id DESC LIMIT $3`, before, action, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) {
		var e Entry
		var raw []byte
		err := r.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorName, &e.ActorType, &e.Action, &e.TargetType, &e.TargetID, &e.IP, &e.UserAgent, &raw)
		if err == nil {
			_ = json.Unmarshal(raw, &e.Details)
		}
		return e, err
	})
}
