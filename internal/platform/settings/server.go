package settings

import (
	"context"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const serverKey = "server"

// Server holds instance-wide settings from Admin → System.
type Server struct {
	// PublicURL is the address people use to open Docveta, for links in emails and push
	// notifications sent from background jobs. Remembered automatically from the first visit by
	// a real address when DOCVETA_BASE_URL is only the installer's localhost default.
	PublicURL string `json:"public_url"`
	// AllowLocalTargets lets everyone's notification channels and workflow webhooks reach
	// addresses on the local network. Administrators' own and system channels always may.
	AllowLocalTargets bool `json:"allow_local_targets"`
}

// serverCache keeps Server settings for a minute (they're read on every request).
type serverCache struct {
	mu   sync.Mutex
	v    Server
	at   time.Time
	have bool
}

// ServerSettings returns the instance settings (cached for a minute).
func (s *Store) ServerSettings(ctx context.Context) Server {
	cache := &s.server
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.have && time.Since(cache.at) < time.Minute {
		return cache.v
	}
	var v Server
	if _, err := s.Get(ctx, serverKey, &v); err != nil {
		return cache.v // keep the last good value through a database hiccup
	}
	cache.v, cache.at, cache.have = v, time.Now(), true
	return v
}

// SetServerSettings saves the instance settings.
func (s *Store) SetServerSettings(ctx context.Context, v Server, by uuid.UUID) error {
	v.PublicURL = strings.TrimRight(strings.TrimSpace(v.PublicURL), "/")
	if err := s.Set(ctx, serverKey, v, by); err != nil {
		return err
	}
	cache := &s.server
	cache.mu.Lock()
	cache.v, cache.at, cache.have = v, time.Now(), true
	cache.mu.Unlock()
	return nil
}

// RememberPublicURL stores u as the public address if none is known yet.
func (s *Store) RememberPublicURL(ctx context.Context, u string) {
	cur := s.ServerSettings(ctx)
	if cur.PublicURL != "" || u == "" {
		return
	}
	cur.PublicURL = u
	_ = s.SetServerSettings(ctx, cur, uuid.Nil)
}

// IsLoopback reports whether a URL's host is this computer (localhost, 127.0.0.1, ::1).
func IsLoopback(u *url.URL) bool {
	h := u.Hostname()
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// PublicURL is the address to put in links sent from background jobs: DOCVETA_BASE_URL when it
// names a real host, otherwise the address administrators set or Docveta remembered.
func (s *Store) PublicURL(ctx context.Context, base *url.URL) string {
	if base != nil && !IsLoopback(base) {
		return strings.TrimRight(base.String(), "/")
	}
	if u := s.ServerSettings(ctx).PublicURL; u != "" {
		return u
	}
	if base == nil {
		return ""
	}
	return strings.TrimRight(base.String(), "/")
}
