package api

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

// maxArchiveDocuments bounds one ZIP download.
const maxArchiveDocuments = 500

// registerArchive adds the download of several documents as one ZIP file.
func (a *API) registerArchive(mux router) {
	mux.HandleFunc("POST /api/v1/documents/archive", a.archive)
}

// archive streams the chosen documents as one ZIP. The body is JSON ({"ids": [...], "kind":
// "original" | "archive"}) or an ordinary form post (ids, kind), so a browser can save the
// download straight to disk without holding it in memory first. "archive" takes the searchable
// PDF where a document has one. Documents the caller can't open are left out; the ZIP says how many.
func (a *API) archive(w http.ResponseWriter, r *http.Request) {
	p := user(w, r)
	if p == nil {
		return
	}
	var raw []string
	kind := "original"
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		in, err := decode[struct {
			IDs  []string `json:"ids"`
			Kind string   `json:"kind"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		raw, kind = in.IDs, in.Kind
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			httpx.Error(w, r, apperr.Invalid("ids", "Choose the documents to download"))
			return
		}
		raw, kind = r.PostForm["ids"], r.PostFormValue("kind")
	}
	if kind == "" {
		kind = "original"
	}
	if kind != "original" && kind != "archive" {
		httpx.Error(w, r, apperr.Invalid("kind", "Must be original or archive"))
		return
	}
	all, err := uuidList(raw)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, id := range all {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		httpx.Error(w, r, apperr.Invalid("ids", "Choose the documents to download"))
		return
	}
	if len(ids) > maxArchiveDocuments {
		httpx.Error(w, r, apperr.Invalid("ids", fmt.Sprintf("Choose up to %d documents at a time", maxArchiveDocuments)))
		return
	}

	open := func(id uuid.UUID) (*documents.File, error) {
		if kind == "archive" {
			if f, err := a.Documents.OpenFile(r.Context(), p, id, "archive"); err == nil || !apperr.IsKind(err, apperr.KindNotFound) {
				return f, err
			}
		}
		return a.Documents.OpenFile(r.Context(), p, id, "original")
	}

	var zw *zip.Writer
	names := map[string]int{}
	left := 0
	for _, id := range ids {
		f, err := open(id)
		if err != nil {
			if zw == nil && !apperr.IsKind(err, apperr.KindNotFound) && !apperr.IsKind(err, apperr.KindForbidden) {
				httpx.Error(w, r, err) // nothing sent yet: say what's wrong
				return
			}
			left++ // gone, not shared with this person, or unreadable just now
			continue
		}
		if zw == nil {
			h := w.Header()
			h.Set("Content-Type", "application/zip")
			h.Set("Content-Disposition", `attachment; filename="docveta-documents-`+time.Now().Format("2006-01-02")+`.zip"`)
			h.Set("Cache-Control", "no-store")
			zw = zip.NewWriter(w)
		}
		// PDFs and pictures are compressed already: store them as they are.
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: archiveName(f.Name, names), Method: zip.Store, Modified: f.Modified})
		if err == nil {
			_, err = io.Copy(fw, f.Reader)
		}
		f.Reader.Close()
		if err != nil {
			httpx.Logger(r.Context()).Warn("zip download stopped", "err", err) // most often the browser went away
			return
		}
	}
	if zw == nil {
		httpx.Error(w, r, apperr.NotFound("Documents"))
		return
	}
	if left > 0 {
		if fw, err := zw.Create("Not included.txt"); err == nil {
			fmt.Fprintf(fw, "%d of the %d documents you chose could not be included: they were deleted, or are no longer shared with you.\r\n", left, len(ids))
		}
	}
	if err := zw.Close(); err != nil {
		httpx.Logger(r.Context()).Warn("zip download stopped", "err", err)
	}
}

// archiveName makes a file name safe inside a ZIP and unique within it ("bill.pdf", "bill (2).pdf").
func archiveName(name string, used map[string]int) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name))
	name = strings.TrimLeft(name, ". ")
	if name == "" {
		name = "document"
	}
	key := strings.ToLower(name)
	used[key]++
	if n := used[key]; n > 1 {
		ext := path.Ext(name)
		name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
		used[strings.ToLower(name)]++
	}
	return name
}
