package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/search"
)

func uuidList(vals []string) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, v := range vals {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, apperr.Invalid("id", "Invalid id "+s)
			}
			out = append(out, id)
		}
	}
	return out, nil
}

// parseQuery reads search parameters from the URL.
func parseQuery(r *http.Request) (search.Query, error) {
	v := r.URL.Query()
	q := search.Query{Q: v.Get("q"), Sort: v.Get("sort"), Cursor: v.Get("cursor"), WithTotal: v.Get("total") != "false"}
	q.Limit = httpx.QueryInt(r, "limit", 50, 1, 200)
	var err error
	for _, f := range []struct {
		name string
		dst  *[]uuid.UUID
	}{{"space_id", &q.SpaceIDs}, {"tag_id", &q.TagIDs}, {"any_tag_id", &q.AnyTagIDs}, {"not_tag_id", &q.NotTagIDs},
		{"correspondent_id", &q.CorrespondentIDs}, {"document_type_id", &q.TypeIDs}} {
		if *f.dst, err = uuidList(v[f.name]); err != nil {
			return q, err
		}
	}
	str := func(k string) *string {
		if s := v.Get(k); s != "" {
			return &s
		}
		return nil
	}
	q.DateFrom, q.DateTo = str("date_from"), str("date_to")
	for _, k := range []struct {
		name string
		dst  **time.Time
	}{{"added_from", &q.AddedFrom}, {"added_to", &q.AddedTo}} {
		if s := v.Get(k.name); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.Parse("2006-01-02", s); err != nil {
					return q, apperr.Invalid(k.name, "Use RFC 3339 or YYYY-MM-DD")
				}
			}
			*k.dst = &t
		}
	}
	if s := v.Get("inbox"); s != "" {
		b := s == "true" || s == "1"
		q.Inbox = &b
	}
	if s := v.Get("status"); s != "" {
		q.Statuses = strings.Split(s, ",")
	}
	q.Untagged = v.Get("untagged") == "true"
	q.NoCorrespondent = v.Get("no_correspondent") == "true"
	q.NoType = v.Get("no_document_type") == "true"
	q.Trash = v.Get("trash") == "true"
	return q, nil
}

func (a *API) registerDocuments(mux router) {
	mux.HandleFunc("GET /api/v1/documents", handle(func(r *http.Request, p *auth.Principal) (*documents.ListResult, error) {
		q, err := parseQuery(r)
		if err != nil {
			return nil, err
		}
		return a.Documents.List(r.Context(), p, q)
	}))
	mux.HandleFunc("POST /api/v1/documents/search", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		q, err := decode[search.Query](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		q.WithTotal = true
		res, err := a.Documents.List(r.Context(), p, q)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("GET /api/v1/documents/suggest", handle(func(r *http.Request, p *auth.Principal) (list[search.Suggestion], error) {
		s, err := a.Search.Suggest(r.Context(), p, r.URL.Query().Get("q"), httpx.QueryInt(r, "limit", 8, 1, 20))
		return items(s), err
	}))
	mux.HandleFunc("GET /api/v1/documents/stats", handle(func(r *http.Request, p *auth.Principal) (*documents.Stats, error) {
		return a.Documents.Stats(r.Context(), p)
	}))
	mux.HandleFunc("POST /api/v1/documents", a.upload)
	mux.HandleFunc("POST /api/v1/documents/bulk", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		in, err := decode[documents.BulkInput](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		res, err := a.Documents.Bulk(r.Context(), p, in)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /api/v1/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		d, err := a.Documents.Get(r.Context(), p, id)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+strconv.Itoa(d.Version)+`"`)
		httpx.JSON(w, http.StatusOK, d)
	})
	mux.HandleFunc("PATCH /api/v1/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		in, err := decode[documents.UpdateInput](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		var ifMatch *int
		if h := strings.Trim(r.Header.Get("If-Match"), `"W/ `); h != "" && h != "*" {
			v, err := strconv.Atoi(h)
			if err != nil {
				httpx.Error(w, r, apperr.Invalid("If-Match", "Invalid version"))
				return
			}
			ifMatch = &v
		}
		d, err := a.Documents.Update(r.Context(), p, id, in, ifMatch)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("ETag", `"`+strconv.Itoa(d.Version)+`"`)
		httpx.JSON(w, http.StatusOK, d)
	})
	mux.HandleFunc("DELETE /api/v1/documents/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		if r.URL.Query().Get("permanent") == "true" {
			return a.Documents.Purge(r.Context(), p, id)
		}
		return a.Documents.Trash(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/restore", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Documents.Restore(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/reprocess", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		profile := ""
		if r.URL.Query().Get("force_ocr") == "true" {
			profile = "force-ocr"
		}
		return a.Documents.Reprocess(r.Context(), p, id, profile)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/asn", handle(func(r *http.Request, p *auth.Principal) (*documents.Document, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.Documents.AssignASN(r.Context(), p, id)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/file", a.serveFile)
	mux.HandleFunc("GET /api/v1/documents/{id}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("kind", "thumbnail")
		r.URL.RawQuery = q.Encode()
		a.serveFile(w, r)
	})
	mux.HandleFunc("GET /api/v1/documents/{id}/pages", handle(func(r *http.Request, p *auth.Principal) (list[documents.Page], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[documents.Page]{}, err
		}
		pg, err := a.Documents.Pages(r.Context(), p, id)
		return items(pg), err
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/notes", handle(func(r *http.Request, p *auth.Principal) (list[documents.Note], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[documents.Note]{}, err
		}
		n, err := a.Documents.Notes(r.Context(), p, id)
		return items(n), err
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/notes", handle(func(r *http.Request, p *auth.Principal) (*documents.Note, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[struct {
			Body string `json:"body"`
		}](r)
		if err != nil {
			return nil, err
		}
		return a.Documents.AddNote(r.Context(), p, id, in.Body)
	}))
	mux.HandleFunc("DELETE /api/v1/documents/{id}/notes/{note}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		note, err := httpx.PathUUID(r, "note")
		if err != nil {
			return err
		}
		return a.Documents.DeleteNote(r.Context(), p, id, note)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/history", handle(func(r *http.Request, p *auth.Principal) (list[documents.HistoryEntry], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[documents.HistoryEntry]{}, err
		}
		h, err := a.Documents.History(r.Context(), p, id)
		return items(h), err
	}))
}

// serveFile streams a document file with Range support and caching headers.
func (a *API) serveFile(w http.ResponseWriter, r *http.Request) {
	p := user(w, r)
	if p == nil {
		return
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "", "original":
		kind = "original"
	case "archive", "thumbnail":
	case "best": // archive when available, else original
		kind = "archive"
		f, err := a.Documents.OpenFile(r.Context(), p, id, kind)
		if apperr.IsKind(err, apperr.KindNotFound) {
			kind = "original"
		} else if err == nil {
			f.Reader.Close()
		}
	default:
		httpx.Error(w, r, apperr.Invalid("kind", "Must be original, archive, thumbnail or best"))
		return
	}
	f, err := a.Documents.OpenFile(r.Context(), p, id, kind)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer f.Reader.Close()
	h := w.Header()
	h.Set("Content-Type", f.Mime)
	h.Set("ETag", f.ETag)
	h.Set("X-Content-Type-Options", "nosniff")
	// Files are private: cache only in the user's browser. Content is addressed by
	// version via ETag; thumbnails change rarely.
	h.Set("Cache-Control", "private, max-age=300")
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	// Only render types the browser handles safely inline; never HTML/SVG (not accepted at ingest anyway).
	if !strings.HasPrefix(f.Mime, "image/") && f.Mime != "application/pdf" && f.Mime != "text/plain" {
		disp = "attachment"
	}
	if f.Mime == "text/plain" {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": f.Name}))
	http.ServeContent(w, r, "", f.Modified, f.Reader)
}

// upload accepts multipart/form-data with metadata fields followed by one "file" part.
// The file is streamed straight to storage; nothing is buffered in memory.
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	p := user(w, r)
	if p == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Error(w, r, apperr.Invalid("body", "Send the file as multipart/form-data"))
		return
	}
	in := documents.IngestInput{Source: "web"}
	if p.Kind == auth.KindToken {
		in.Source = "api"
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			httpx.Error(w, r, apperr.Invalid("file", "No file was uploaded"))
			return
		}
		if err != nil {
			httpx.Error(w, r, apperr.Invalid("body", "Malformed upload"))
			return
		}
		name := part.FormName()
		if name == "file" {
			if in.SpaceID == uuid.Nil {
				// Default to the user's personal space.
				sp, err := a.Spaces.List(r.Context(), p)
				if err != nil || len(sp) == 0 {
					httpx.Error(w, r, apperr.Invalid("space_id", "Choose a space"))
					return
				}
				in.SpaceID = sp[0].ID
			}
			in.Filename = part.FileName()
			doc, err := a.Documents.Ingest(r.Context(), p, in, part)
			part.Close()
			if err != nil {
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					err = &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large", Msg: "This file is too large"}
				}
				httpx.Error(w, r, err)
				return
			}
			httpx.JSON(w, http.StatusCreated, doc)
			return
		}
		val, err := io.ReadAll(io.LimitReader(part, 64<<10))
		part.Close()
		if err != nil {
			httpx.Error(w, r, apperr.Invalid(name, "Could not read field"))
			return
		}
		s := strings.TrimSpace(string(val))
		switch name {
		case "space_id":
			if in.SpaceID, err = uuid.Parse(s); err != nil {
				httpx.Error(w, r, apperr.Invalid("space_id", "Invalid id"))
				return
			}
		case "title":
			in.Title = s
		case "tag_ids":
			ids, err := uuidList([]string{s})
			if err != nil {
				httpx.Error(w, r, err)
				return
			}
			in.TagIDs = append(in.TagIDs, ids...)
		case "correspondent_id", "document_type_id":
			if s == "" {
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				httpx.Error(w, r, apperr.Invalid(name, "Invalid id"))
				return
			}
			if name == "correspondent_id" {
				in.CorrespondentID = &id
			} else {
				in.DocumentTypeID = &id
			}
		case "document_date":
			if s != "" {
				in.DocumentDate = &s
			}
		case "language":
			in.Language = s
		case "allow_duplicate":
			in.AllowDuplicate = s == "true" || s == "1"
		case "source":
			if s == "share" || s == "scan" {
				in.Source = s
			}
		}
	}
}
