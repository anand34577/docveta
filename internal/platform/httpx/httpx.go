// Package httpx contains HTTP helpers shared by all handlers: JSON encoding,
// RFC 9457 problem responses, request decoding and common middleware.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
)

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Problem is an RFC 9457 problem details body.
type Problem struct {
	Type     string              `json:"type"`
	Title    string              `json:"title"`
	Status   int                 `json:"status"`
	Detail   string              `json:"detail,omitempty"`
	Code     string              `json:"code"`
	Errors   []apperr.FieldError `json:"errors,omitempty"`
	Instance string              `json:"instance,omitempty"`
	Extra    map[string]any      `json:"extra,omitempty"`
}

// Error converts err into a problem response. Unknown errors are logged and hidden.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ae, ok := apperr.As(err)
	if !ok {
		if errors.Is(err, context.Canceled) {
			return // client went away
		}
		ae = apperr.Internal(err)
	}
	status := ae.Status()
	if status >= 500 {
		Logger(r.Context()).Error("request failed", "err", err, "path", r.URL.Path)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:     "https://docveta.dev/problems/" + ae.Code,
		Title:    ae.Msg,
		Status:   status,
		Code:     ae.Code,
		Errors:   ae.Fields,
		Instance: r.URL.Path,
		Extra:    ae.Extra,
	})
}

// Decode reads a JSON body (max 1 MiB) into v, rejecting unknown fields.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &typ):
			return apperr.Invalid(typ.Field, "has the wrong type")
		case errors.As(err, &syn), errors.Is(err, io.ErrUnexpectedEOF):
			return &apperr.Error{Kind: apperr.KindValidation, Code: "bad_json", Msg: "Request body is not valid JSON"}
		case errors.Is(err, io.EOF):
			return &apperr.Error{Kind: apperr.KindValidation, Code: "empty_body", Msg: "Request body is empty"}
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return &apperr.Error{Kind: apperr.KindValidation, Code: "unknown_field", Msg: strings.TrimPrefix(err.Error(), "json: ")}
		}
		return &apperr.Error{Kind: apperr.KindValidation, Code: "bad_json", Msg: "Request body could not be read", Err: err}
	}
	return nil
}

// PathUUID parses a UUID path value.
func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Resource")
	}
	return id, nil
}

// QueryInt parses an integer query parameter with bounds.
func QueryInt(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max0(min, minN(v, max))
}

func max0(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minN(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

type ctxKey int

const (
	keyLogger ctxKey = iota
	keyRequestID
	keyClientIP
)

// Logger returns the request-scoped logger.
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(keyLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// ClientIP returns the client IP resolved by the RealIP middleware.
func ClientIP(ctx context.Context) string {
	s, _ := ctx.Value(keyClientIP).(string)
	return s
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(s int) {
	if w.status == 0 {
		w.status = s
	}
	w.ResponseWriter.WriteHeader(s)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Observe is called after each request with route pattern, status and duration.
type Observe func(pattern, method string, status int, d time.Duration)

// Base installs request ID, client IP, logger, panic recovery and access logging.
func Base(log *slog.Logger, trusted []netip.Prefix, observe Observe) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rid := r.Header.Get("X-Request-ID")
			if rid == "" || len(rid) > 64 {
				rid = uuid.NewString()
			}
			ip := clientIP(r, trusted)
			l := log.With("request_id", rid)
			ctx := context.WithValue(r.Context(), keyLogger, l)
			ctx = context.WithValue(ctx, keyRequestID, rid)
			ctx = context.WithValue(ctx, keyClientIP, ip)
			w.Header().Set("X-Request-ID", rid)
			sw := &statusWriter{ResponseWriter: w}
			r = r.WithContext(ctx)

			defer func() {
				if p := recover(); p != nil {
					l.Error("panic", "panic", p, "stack", string(debug.Stack()))
					if sw.status == 0 {
						Error(sw, r, apperr.Internal(errors.New("panic")))
					}
				}
				d := time.Since(start)
				if observe != nil {
					observe(r.Pattern, r.Method, sw.status, d)
				}
				// Skip noisy health/asset logs at info level.
				lvl := slog.LevelInfo
				if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
					lvl = slog.LevelDebug
				}
				l.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
					"bytes", sw.bytes, "ms", d.Milliseconds(), "ip", ip)
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

func clientIP(r *http.Request, trusted []netip.Prefix) string {
	host := r.RemoteAddr
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		host = ap.Addr().String()
	}
	remote, err := netip.ParseAddr(host)
	if err != nil || !isTrusted(remote, trusted) {
		return host
	}
	// Walk X-Forwarded-For from the right, skipping trusted proxies.
	xff := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(xff) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(xff[i]))
		if err != nil {
			break
		}
		if !isTrusted(a, trusted) {
			return a.String()
		}
	}
	return host
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a.Unmap()) {
			return true
		}
	}
	return false
}

// SecurityHeaders sets conservative security headers on every response.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'wasm-unsafe-eval'", // pdf.js decodes JPEG2000 via WASM
		"style-src 'self' 'unsafe-inline'",     // Radix/inline style attributes
		"img-src 'self' data: blob:",
		"font-src 'self'",
		"connect-src 'self'",
		"worker-src 'self' blob:",
		"frame-src 'self' blob:",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Chain applies middlewares so that the first one is outermost.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// FromTrustedProxy reports whether the request came straight from a trusted reverse proxy,
// whose X-Forwarded-* headers can then be believed.
func FromTrustedProxy(r *http.Request, trusted []netip.Prefix) bool {
	host := r.RemoteAddr
	if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		host = ap.Addr().String()
	}
	a, err := netip.ParseAddr(host)
	return err == nil && isTrusted(a, trusted)
}
