package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/workflows"
)

func (a *API) registerWorkflows(mux router) {
	mux.HandleFunc("GET /api/v1/workflows", handle(func(r *http.Request, p *auth.Principal) (list[*workflows.Workflow], error) {
		var sp *uuid.UUID
		if s := r.URL.Query().Get("space_id"); s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return list[*workflows.Workflow]{}, apperr.Invalid("space_id", "Invalid id")
			}
			sp = &id
		}
		l, err := a.Workflows.List(r.Context(), p, sp)
		return items(l), err
	}))
	mux.HandleFunc("POST /api/v1/workflows", handle(func(r *http.Request, p *auth.Principal) (*workflows.Workflow, error) {
		in, err := decode[workflows.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Workflows.Create(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/workflows/{id}", handleAction(func(r *http.Request, p *auth.Principal) (*workflows.Workflow, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[workflows.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Workflows.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/workflows/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Workflows.Delete(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/workflows/{id}/test", handleAction(func(r *http.Request, p *auth.Principal) (*workflows.TestResult, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[struct {
			DocumentID uuid.UUID `json:"document_id"`
		}](r)
		if err != nil {
			return nil, err
		}
		return a.Workflows.Test(r.Context(), p, id, in.DocumentID)
	}))
	mux.HandleFunc("GET /api/v1/workflows/{id}/runs", handle(func(r *http.Request, p *auth.Principal) (list[workflows.Run], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[workflows.Run]{}, err
		}
		l, err := a.Workflows.Runs(r.Context(), p, id)
		return items(l), err
	}))
}
