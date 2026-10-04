// Package webui serves the embedded React single-page app. The frontend build
// (web/) writes its output into dist/ here, so the Go binary is self-contained.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const placeholder = `<!doctype html><meta charset="utf-8"><title>Docveta</title>
<body style="font-family:system-ui;padding:40px"><h1>Docveta API is running</h1>
<p>The web interface hasn't been built into this binary. Run <code>npm run build</code> in <code>web/</code>, then rebuild.</p></body>`

// Handler serves static assets with long-lived caching and falls back to index.html
// for client-side routes.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		index = []byte(placeholder)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(sub, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					// Vite emits content-hashed filenames.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasPrefix(p, "s/") { // public share pages: keep them out of search engines
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		w.Write(index)
	})
}
