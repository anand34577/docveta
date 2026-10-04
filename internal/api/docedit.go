package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/docedit"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

// registerDocEdit adds the routes that change a document's file: unlock, page edits,
// split, merge and versions.
func (a *API) registerDocEdit(mux router) {
	mux.HandleFunc("POST /api/v1/documents/{id}/unlock", handleAction(func(r *http.Request, p *auth.Principal) (*documents.Document, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[struct {
			Password string `json:"password"`
		}](r)
		if err != nil {
			return nil, err
		}
		d, err := a.Docedit.Unlock(r.Context(), p, id, in.Password)
		if err == nil {
			a.Audit.Record(r.Context(), nil, "document.unlock", "document", id.String(), nil)
		}
		return d, err
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/pages/edit", handleAction(func(r *http.Request, p *auth.Principal) (*documents.Document, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[struct {
			Pages []docedit.PageOp `json:"pages"`
		}](r)
		if err != nil {
			return nil, err
		}
		return a.Docedit.EditPages(r.Context(), p, id, in.Pages)
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/split", handle(func(r *http.Request, p *auth.Principal) (list[*documents.Document], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[*documents.Document]{}, err
		}
		in, err := decode[struct {
			Ranges        []string `json:"ranges"`
			TrashOriginal bool     `json:"trash_original"`
		}](r)
		if err != nil {
			return list[*documents.Document]{}, err
		}
		docs, err := a.Docedit.Split(r.Context(), p, id, in.Ranges, in.TrashOriginal)
		return items(docs), err
	}))
	mux.HandleFunc("POST /api/v1/documents/merge", handle(func(r *http.Request, p *auth.Principal) (*documents.Document, error) {
		in, err := decode[struct {
			IDs            []uuid.UUID `json:"ids"`
			Title          string      `json:"title"`
			TrashOriginals bool        `json:"trash_originals"`
		}](r)
		if err != nil {
			return nil, err
		}
		return a.Docedit.Merge(r.Context(), p, in.IDs, in.Title, in.TrashOriginals)
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/versions", handle(func(r *http.Request, p *auth.Principal) (list[documents.VersionInfo], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[documents.VersionInfo]{}, err
		}
		v, err := a.Documents.Versions(r.Context(), p, id)
		return items(v), err
	}))
	mux.HandleFunc("POST /api/v1/documents/{id}/versions", a.uploadVersion)
	mux.HandleFunc("POST /api/v1/documents/{id}/versions/{no}/restore", handleAction(func(r *http.Request, p *auth.Principal) (*documents.Document, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		no, err := strconv.Atoi(r.PathValue("no"))
		if err != nil || no < 1 {
			return nil, apperr.Invalid("no", "Invalid version")
		}
		return a.Documents.RestoreVersion(r.Context(), p, id, no)
	}))
	mux.HandleFunc("DELETE /api/v1/trash", handle(func(r *http.Request, p *auth.Principal) (*documents.EmptyTrashResult, error) {
		return a.Documents.EmptyTrash(r.Context(), p)
	}))
}

// uploadVersion puts a new file in an existing document (multipart: optional "note", then "file").
func (a *API) uploadVersion(w http.ResponseWriter, r *http.Request) {
	p := user(w, r)
	if p == nil {
		return
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.MaxUploadBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Error(w, r, apperr.Invalid("body", "Send the file as multipart/form-data"))
		return
	}
	note := "Replaced file"
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
		if part.FormName() == "file" {
			d, err := a.Documents.AddVersion(r.Context(), p, id, part, note)
			part.Close()
			if err != nil {
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					err = &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large", Msg: "This file is too large"}
				}
				httpx.Error(w, r, err)
				return
			}
			httpx.JSON(w, http.StatusCreated, d)
			return
		}
		if part.FormName() == "note" {
			b, _ := io.ReadAll(io.LimitReader(part, 500))
			if s := strings.TrimSpace(string(b)); s != "" {
				note = s
			}
		}
		part.Close()
	}
}
