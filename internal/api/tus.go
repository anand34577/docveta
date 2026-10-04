package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/tus"
)

// registerUploads serves the tus resumable upload protocol at /api/v1/uploads.
func (a *API) registerUploads(mux router) {
	base := "/api/v1/uploads"
	common := func(w http.ResponseWriter) {
		w.Header().Set("Tus-Resumable", tus.Version)
		w.Header().Set("Cache-Control", "no-store")
	}
	// Every tus request (except OPTIONS) must name the protocol version it speaks.
	versioned := func(w http.ResponseWriter, r *http.Request) bool {
		common(w)
		if r.Header.Get("Tus-Resumable") != tus.Version {
			w.Header().Set("Tus-Version", tus.Version)
			httpx.Error(w, r, &apperr.Error{Kind: apperr.KindPrecondition, Code: "tus_version", Msg: "This server speaks tus " + tus.Version})
			return false
		}
		return true
	}
	mux.HandleFunc("OPTIONS "+base, func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Tus-Resumable", tus.Version)
		h.Set("Tus-Version", tus.Version)
		h.Set("Tus-Extension", "creation,termination,expiration")
		h.Set("Tus-Max-Size", strconv.FormatInt(a.Uploads.MaxSize(), 10))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil || !versioned(w, r) {
			return
		}
		length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
		if err != nil || length < 0 {
			httpx.Error(w, r, apperr.Invalid("Upload-Length", "Send the file size in the Upload-Length header"))
			return
		}
		meta, err := tus.ParseMetadata(r.Header.Get("Upload-Metadata"))
		if err != nil {
			httpx.Error(w, r, apperr.Invalid("Upload-Metadata", err.Error()))
			return
		}
		u, err := a.Uploads.Create(r.Context(), p, length, meta)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("Location", base+"/"+u.ID.String())
		w.Header().Set("Upload-Expires", u.ExpiresAt.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusCreated)
	})
	id := func(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
		v, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, apperr.NotFound("Upload"))
			return uuid.Nil, false
		}
		return v, true
	}
	mux.HandleFunc("HEAD "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		uid, ok := id(w, r)
		if p == nil || !ok || !versioned(w, r) {
			return
		}
		u, err := a.Uploads.Head(r.Context(), p, uid)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		h := w.Header()
		h.Set("Upload-Offset", strconv.FormatInt(u.Offset, 10))
		h.Set("Upload-Length", strconv.FormatInt(u.Length, 10))
		h.Set("Upload-Expires", u.ExpiresAt.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("PATCH "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		uid, ok := id(w, r)
		if p == nil || !ok || !versioned(w, r) {
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/offset+octet-stream") {
			httpx.Error(w, r, &apperr.Error{Kind: apperr.KindUnsupported, Code: "bad_content_type", Msg: "Send chunks as application/offset+octet-stream"})
			return
		}
		offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
		if err != nil || offset < 0 {
			httpx.Error(w, r, apperr.Invalid("Upload-Offset", "Send the offset in the Upload-Offset header"))
			return
		}
		n, doc, err := a.Uploads.Patch(r.Context(), p, uid, offset, r.Body)
		if err != nil {
			if errors.Is(err, tus.ErrOffset) {
				w.Header().Set("Upload-Offset", strconv.FormatInt(n, 10))
			}
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) {
				return // the client's connection dropped mid-chunk; what arrived is kept, it will resume
			}
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				err = &apperr.Error{Kind: apperr.KindTooLarge, Code: "file_too_large", Msg: "This file is too large"}
			}
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("Upload-Offset", strconv.FormatInt(n, 10))
		w.Header().Set("Upload-Expires", time.Now().Add(24*time.Hour).UTC().Format(http.TimeFormat))
		if doc != nil {
			w.Header().Set("Docveta-Document-Id", doc.ID.String())
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		uid, ok := id(w, r)
		if p == nil || !ok || !versioned(w, r) {
			return
		}
		if err := a.Uploads.Delete(r.Context(), p, uid); err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
