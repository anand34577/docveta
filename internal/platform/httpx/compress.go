package httpx

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Responses shorter than this aren't worth a gzip header (when the length is known up front).
const compressMinBytes = 1024

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, 5) // close to the default's size at about half the CPU
	return w
}}

// Compress gzips text responses (the web app's scripts and styles, JSON) for clients that
// accept it: the app is about a third of its size on the wire, which is most of a first load
// over a slow or remote connection. Files that are compressed already (PDFs, pictures, ZIP
// exports), event streams, partial (Range) responses and HEAD requests are left alone.
func Compress() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Accept-Encoding")
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" || r.Header.Get("Upgrade") != "" || !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}
			cw := &compressWriter{ResponseWriter: w}
			defer cw.finish()
			next.ServeHTTP(cw, r)
		})
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			continue
		}
		q := strings.TrimSpace(params)
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

func compressible(contentType string) bool {
	t, _, _ := strings.Cut(contentType, ";")
	t = strings.ToLower(strings.TrimSpace(t))
	switch {
	case t == "text/event-stream": // live events and streamed answers must arrive as they're written
		return false
	case strings.HasPrefix(t, "text/"), strings.HasSuffix(t, "+json"), strings.HasSuffix(t, "+xml"):
		return true
	}
	switch t {
	case "application/json", "application/javascript", "application/xml", "application/yaml", "application/x-yaml", "application/wasm":
		return true
	}
	return false
}

type compressWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

// decide runs once, when the status and headers are final.
func (w *compressWriter) decide(status int) {
	w.decided = true
	h := w.Header()
	if status < 200 || status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusPartialContent ||
		h.Get("Content-Encoding") != "" || h.Get("Content-Range") != "" || !compressible(h.Get("Content-Type")) {
		return
	}
	if n, err := strconv.Atoi(h.Get("Content-Length")); err == nil && n < compressMinBytes {
		return
	}
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	w.gz = gzipPool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
}

func (w *compressWriter) WriteHeader(status int) {
	if w.decided {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status) // informational: the real status follows
		return
	}
	w.decide(status)
	w.ResponseWriter.WriteHeader(status)
}

func (w *compressWriter) Write(b []byte) (int, error) {
	if !w.decided {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.gz != nil {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *compressWriter) Flush() {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *compressWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *compressWriter) finish() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	w.gz.Reset(io.Discard)
	gzipPool.Put(w.gz)
	w.gz = nil
}
