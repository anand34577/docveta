// Package ratelimit provides a small in-memory fixed-window limiter for abuse-prone
// endpoints (login, share links). A single Docveta instance is the deployment model, so
// in-memory state is sufficient.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*bucket
	last   time.Time
}

type bucket struct {
	count int
	reset time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, hits: map[string]*bucket{}}
}

// Allow records a hit for key and reports whether it is within the limit, plus the
// time until the window resets.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.last) > l.window {
		for k, b := range l.hits {
			if now.After(b.reset) {
				delete(l.hits, k)
			}
		}
		l.last = now
	}
	b, ok := l.hits[key]
	if !ok || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.count++
	return b.count <= l.limit, b.reset.Sub(now)
}

// Reset clears a key (e.g. after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}
