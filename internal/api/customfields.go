package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/customfields"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

func (a *API) registerCustomFields(mux router) {
	mux.HandleFunc("GET /api/v1/custom-fields", handle(func(r *http.Request, p *auth.Principal) (list[*customfields.Field], error) {
		var sp *uuid.UUID
		if s := r.URL.Query().Get("space_id"); s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return list[*customfields.Field]{}, apperr.Invalid("space_id", "Invalid id")
			}
			sp = &id
		}
		f, err := a.CustomFields.List(r.Context(), p, sp)
		return items(f), err
	}))
	mux.HandleFunc("POST /api/v1/custom-fields", handle(func(r *http.Request, p *auth.Principal) (*customfields.Field, error) {
		in, err := decode[struct {
			SpaceID uuid.UUID `json:"space_id"`
			customfields.Input
		}](r)
		if err != nil {
			return nil, err
		}
		return a.CustomFields.Create(r.Context(), p, in.SpaceID, in.Input)
	}))
	mux.HandleFunc("PATCH /api/v1/custom-fields/{id}", handleAction(func(r *http.Request, p *auth.Principal) (*customfields.Field, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[customfields.Input](r)
		if err != nil {
			return nil, err
		}
		return a.CustomFields.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/custom-fields/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.CustomFields.Delete(r.Context(), p, id)
	}))
}
