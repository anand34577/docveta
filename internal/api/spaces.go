package api

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/taxonomy"
	"github.com/anand34577/docveta/internal/views"
)

func (a *API) registerSpaces(mux router) {
	mux.HandleFunc("GET /api/v1/spaces", handle(func(r *http.Request, p *auth.Principal) (list[*spaces.Space], error) {
		s, err := a.Spaces.List(r.Context(), p)
		return items(s), err
	}))
	mux.HandleFunc("POST /api/v1/spaces", handle(func(r *http.Request, p *auth.Principal) (*spaces.Space, error) {
		in, err := decode[spaces.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Spaces.Create(r.Context(), p, in)
	}))
	mux.HandleFunc("GET /api/v1/spaces/{id}", handle(func(r *http.Request, p *auth.Principal) (*spaces.Space, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.Spaces.Get(r.Context(), p, id)
	}))
	mux.HandleFunc("PATCH /api/v1/spaces/{id}", handle(func(r *http.Request, p *auth.Principal) (*spaces.Space, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[spaces.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Spaces.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/spaces/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Spaces.Delete(r.Context(), p, id)
	}))
	mux.HandleFunc("GET /api/v1/spaces/{id}/members", handle(func(r *http.Request, p *auth.Principal) (list[spaces.Member], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[spaces.Member]{}, err
		}
		m, err := a.Spaces.Members(r.Context(), p, id)
		return items(m), err
	}))
	mux.HandleFunc("PUT /api/v1/spaces/{id}/members/{user}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		uid, err := httpx.PathUUID(r, "user")
		if err != nil {
			return err
		}
		in, err := decode[struct {
			Role spaces.Role `json:"role"`
		}](r)
		if err != nil {
			return err
		}
		if err := a.Spaces.SetMember(r.Context(), p, id, uid, in.Role); err != nil {
			return err
		}
		a.Audit.Record(r.Context(), nil, "space.member_set", "space", id.String(), map[string]any{"user": uid, "role": in.Role})
		return nil
	}))
	mux.HandleFunc("DELETE /api/v1/spaces/{id}/members/{user}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		uid, err := httpx.PathUUID(r, "user")
		if err != nil {
			return err
		}
		if err := a.Spaces.RemoveMember(r.Context(), p, id, uid); err != nil {
			return err
		}
		a.Audit.Record(r.Context(), nil, "space.member_remove", "space", id.String(), map[string]any{"user": uid})
		return nil
	}))

	// Saved views
	mux.HandleFunc("GET /api/v1/saved-views", handle(func(r *http.Request, p *auth.Principal) (list[views.View], error) {
		v, err := a.Views.List(r.Context(), p)
		return items(v), err
	}))
	mux.HandleFunc("POST /api/v1/saved-views", handle(func(r *http.Request, p *auth.Principal) (*views.View, error) {
		in, err := decode[views.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Views.Create(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/saved-views/{id}", handle(func(r *http.Request, p *auth.Principal) (*views.View, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[views.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Views.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/saved-views/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Views.Delete(r.Context(), p, id)
	}))

	// Delta sync
	mux.HandleFunc("GET /api/v1/changes", handle(func(r *http.Request, p *auth.Principal) (*views.ChangesPage, error) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64) // int64: change_seq outgrows int on 32-bit
		since = max(since, 0)
		return views.Changes(r.Context(), a.Pool, a.Spaces, p, since, httpx.QueryInt(r, "limit", 500, 1, 2000))
	}))
}

var taxonomyKinds = map[string]taxonomy.Kind{
	"tags":           taxonomy.Tags,
	"correspondents": taxonomy.Correspondents,
	"document-types": taxonomy.DocumentTypes,
}

func (a *API) registerTaxonomy(mux router) {
	for path, kind := range taxonomyKinds {
		base := "/api/v1/" + path
		mux.HandleFunc("GET "+base, handle(func(r *http.Request, p *auth.Principal) (list[*taxonomy.Item], error) {
			var sp *uuid.UUID
			if s := r.URL.Query().Get("space_id"); s != "" {
				id, err := uuid.Parse(s)
				if err != nil {
					return list[*taxonomy.Item]{}, apperr.Invalid("space_id", "Invalid id")
				}
				sp = &id
			}
			l, err := a.Taxonomy.List(r.Context(), p, kind, sp)
			return items(l), err
		}))
		mux.HandleFunc("POST "+base, handle(func(r *http.Request, p *auth.Principal) (*taxonomy.Item, error) {
			in, err := decode[struct {
				SpaceID uuid.UUID `json:"space_id"`
				taxonomy.Input
			}](r)
			if err != nil {
				return nil, err
			}
			return a.Taxonomy.Create(r.Context(), p, kind, in.SpaceID, in.Input)
		}))
		mux.HandleFunc("GET "+base+"/{id}", handle(func(r *http.Request, p *auth.Principal) (*taxonomy.Item, error) {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return nil, err
			}
			return a.Taxonomy.Get(r.Context(), p, kind, id)
		}))
		mux.HandleFunc("PATCH "+base+"/{id}", handle(func(r *http.Request, p *auth.Principal) (*taxonomy.Item, error) {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return nil, err
			}
			in, err := decode[taxonomy.Input](r)
			if err != nil {
				return nil, err
			}
			return a.Taxonomy.Update(r.Context(), p, kind, id, in)
		}))
		mux.HandleFunc("DELETE "+base+"/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return err
			}
			return a.Taxonomy.Delete(r.Context(), p, kind, id)
		}))
		mux.HandleFunc("POST "+base+"/{id}/merge", handle(func(r *http.Request, p *auth.Principal) (*taxonomy.Item, error) {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return nil, err
			}
			in, err := decode[struct {
				SourceIDs []uuid.UUID `json:"source_ids"`
			}](r)
			if err != nil {
				return nil, err
			}
			return a.Taxonomy.Merge(r.Context(), p, kind, id, in.SourceIDs)
		}))
	}
}
