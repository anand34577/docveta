// Package auth holds the authenticated principal carried through request contexts.
// It is deliberately tiny and dependency-free so every package can import it.
package auth

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

type Kind string

const (
	KindSession Kind = "session"
	KindToken   Kind = "token"
	KindWorker  Kind = "worker"
	KindSystem  Kind = "system"
)

// Scopes for API tokens. Sessions implicitly have all scopes.
const (
	ScopeRead   = "documents:read"
	ScopeWrite  = "documents:write"
	ScopeUpload = "upload"
	ScopeAdmin  = "admin"
)

var AllScopes = []string{ScopeRead, ScopeWrite, ScopeUpload, ScopeAdmin}

type Principal struct {
	Kind      Kind
	UserID    uuid.UUID
	IsAdmin   bool
	Email     string
	Name      string
	SessionID uuid.UUID // for sessions
	TokenID   uuid.UUID // for API tokens
	WorkerID  uuid.UUID // for workers
	Scopes    []string  // for API tokens
}

// Has reports whether the principal may use a capability guarded by scope.
func (p *Principal) Has(scope string) bool {
	if p == nil {
		return false
	}
	switch p.Kind {
	case KindSession, KindSystem:
		return true
	case KindToken:
		return slices.Contains(p.Scopes, scope) ||
			(scope == ScopeRead && slices.Contains(p.Scopes, ScopeWrite))
	}
	return false
}

// Admin reports whether the principal can perform instance administration.
func (p *Principal) Admin() bool {
	return p != nil && p.IsAdmin && p.Has(ScopeAdmin)
}

type ctxKey struct{}

func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal or nil for anonymous requests.
func From(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// System returns a principal for internal jobs.
func System() *Principal {
	return &Principal{Kind: KindSystem, IsAdmin: true}
}
