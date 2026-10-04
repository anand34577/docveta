package api

import (
	"net/http"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/folders"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

func (a *API) registerFolders(mux router) {
	mux.HandleFunc("GET /api/v1/admin/folders", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		l, err := a.Folders.List(r.Context(), p)
		if l == nil {
			l = []*folders.Folder{}
		}
		return map[string]any{"items": l, "roots": a.Folders.Roots()}, err
	}))
	mux.HandleFunc("POST /api/v1/admin/folders", handle(func(r *http.Request, p *auth.Principal) (*folders.Folder, error) {
		in, err := decode[folders.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Folders.Create(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/admin/folders/{id}", handleAction(func(r *http.Request, p *auth.Principal) (*folders.Folder, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[folders.Input](r)
		if err != nil {
			return nil, err
		}
		return a.Folders.Update(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/folders/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Folders.Delete(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/admin/folders/{id}/scan", handleAction(func(r *http.Request, p *auth.Principal) (*folders.ScanResult, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.Folders.ScanNow(r.Context(), p, id)
	}))
}
