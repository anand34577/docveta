package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/collections"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

func (a *API) registerCollections(mux router) {
	mux.HandleFunc("GET /api/v1/collections", handle(func(r *http.Request, p *auth.Principal) (list[collections.Collection], error) {
		var doc *uuid.UUID
		if s := r.URL.Query().Get("document_id"); s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return list[collections.Collection]{}, apperr.Invalid("document_id", "Invalid id")
			}
			doc = &id
		}
		c, err := a.Collections.List(r.Context(), p, doc)
		return items(c), err
	}))
	mux.HandleFunc("POST /api/v1/collections", handle(func(r *http.Request, p *auth.Principal) (*collections.Collection, error) {
		in, err := decode[collections.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Collections.Create(r.Context(), p, in)
	}))
	mux.HandleFunc("GET /api/v1/collections/{id}", handle(func(r *http.Request, p *auth.Principal) (*collections.Collection, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.Collections.Get(r.Context(), p, id)
	}))
	mux.HandleFunc("PATCH /api/v1/collections/{id}", handle(func(r *http.Request, p *auth.Principal) (*collections.Collection, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[collections.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Collections.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/collections/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Collections.Delete(r.Context(), p, id)
	}))
	type idsBody struct {
		IDs []uuid.UUID `json:"ids"`
	}
	type countResult struct {
		Count int `json:"count"`
	}
	for path, fn := range map[string]func(*http.Request, *auth.Principal, uuid.UUID, []uuid.UUID) (int, error){
		"POST /api/v1/collections/{id}/documents": func(r *http.Request, p *auth.Principal, id uuid.UUID, ids []uuid.UUID) (int, error) {
			return a.Collections.Add(r.Context(), p, id, ids)
		},
		"POST /api/v1/collections/{id}/documents/remove": func(r *http.Request, p *auth.Principal, id uuid.UUID, ids []uuid.UUID) (int, error) {
			return a.Collections.Remove(r.Context(), p, id, ids)
		},
	} {
		mux.HandleFunc(path, handleAction(func(r *http.Request, p *auth.Principal) (*countResult, error) {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return nil, err
			}
			in, err := decode[idsBody](r)
			if err != nil {
				return nil, err
			}
			n, err := fn(r, p, id, in.IDs)
			if err != nil {
				return nil, err
			}
			return &countResult{Count: n}, nil
		}))
	}
}
