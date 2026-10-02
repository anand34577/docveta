// Package jobs defines background job arguments and a small wrapper around the River
// client. Jobs are inserted in the same transaction as the data they act on, so state
// and work can never diverge (DESIGN ADR-006, ADR-019).
package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Priorities: River runs priority 1 first.
const (
	PriorityInteractive = 1
	PriorityNormal      = 2
	PriorityBulk        = 3
	PriorityBackground  = 4
)

type PreprocessArgs struct {
	DocumentID uuid.UUID `json:"document_id"`
	Version    int       `json:"version"`
	Profile    string    `json:"profile,omitempty"`
	Priority   int       `json:"priority,omitempty"`
}

func (PreprocessArgs) Kind() string { return "preprocess" }

type OCRFinalizeArgs struct {
	RunID uuid.UUID `json:"run_id"`
}

func (OCRFinalizeArgs) Kind() string { return "ocr_finalize" }

type ReindexArgs struct {
	DocumentIDs []uuid.UUID `json:"document_ids"`
}

func (ReindexArgs) Kind() string { return "reindex" }

type ClassifyArgs struct {
	DocumentID uuid.UUID `json:"document_id"`
	// Finalize marks the document ready and sends "processed" notifications after classifying.
	Finalize bool `json:"finalize"`
}

func (ClassifyArgs) Kind() string { return "classify" }

type NotifyArgs struct {
	Event Event `json:"event"`
}

func (NotifyArgs) Kind() string { return "notify" }

type MaintenanceArgs struct{}

func (MaintenanceArgs) Kind() string { return "maintenance" }

type LeaseReaperArgs struct{}

func (LeaseReaperArgs) Kind() string { return "lease_reaper" }

// Event is a domain event delivered to notification channels.
type Event struct {
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Body       string      `json:"body,omitempty"`
	Link       string      `json:"link,omitempty"`
	Severity   string      `json:"severity,omitempty"` // info | success | warning | error
	Recipients []uuid.UUID `json:"recipients,omitempty"`
	ToAdmins   bool        `json:"to_admins,omitempty"`
	SpaceID    *uuid.UUID  `json:"space_id,omitempty"`
	DocumentID *uuid.UUID  `json:"document_id,omitempty"`
	ActorID    *uuid.UUID  `json:"actor_id,omitempty"`
	At         time.Time   `json:"at"`
}

// Queue is a late-bound handle on the River client (workers are registered before the
// client exists, and workers themselves enqueue follow-up jobs).
type Queue struct {
	Client *river.Client[pgx.Tx]
}

var errNotStarted = errors.New("job queue not initialized")

func (q *Queue) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) error {
	if q == nil || q.Client == nil {
		return errNotStarted
	}
	_, err := q.Client.InsertTx(ctx, tx, args, opts)
	return err
}

func (q *Queue) Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) error {
	if q == nil || q.Client == nil {
		return errNotStarted
	}
	_, err := q.Client.Insert(ctx, args, opts)
	return err
}

// EmitTx enqueues a notification event inside tx.
func (q *Queue) EmitTx(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	return q.InsertTx(ctx, tx, NotifyArgs{Event: e}, &river.InsertOpts{Priority: PriorityNormal, MaxAttempts: 5})
}

// Emit enqueues a notification event outside a transaction.
func (q *Queue) Emit(ctx context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	return q.Insert(ctx, NotifyArgs{Event: e}, &river.InsertOpts{Priority: PriorityNormal, MaxAttempts: 5})
}
