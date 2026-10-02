package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/notify"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

func (a *API) registerNotify(mux router) {
	mux.HandleFunc("GET /api/v1/notifications", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		l, unread, err := a.Notify.List(r.Context(), p, r.URL.Query().Get("unread") == "true", httpx.QueryInt(r, "limit", 30, 1, 200))
		if l == nil {
			l = []notify.Notification{}
		}
		return map[string]any{"items": l, "unread": unread}, err
	}))
	mux.HandleFunc("POST /api/v1/notifications/read", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		in, err := decode[struct {
			IDs []uuid.UUID `json:"ids"`
			All bool        `json:"all"`
		}](r)
		if err != nil {
			return err
		}
		if in.All {
			return a.Notify.MarkRead(r.Context(), p, nil)
		}
		if in.IDs == nil {
			in.IDs = []uuid.UUID{}
		}
		return a.Notify.MarkRead(r.Context(), p, in.IDs)
	}))
	mux.HandleFunc("GET /api/v1/notification-channels", handle(func(r *http.Request, p *auth.Principal) (map[string]any, error) {
		c, err := a.Notify.Channels(r.Context(), p)
		return map[string]any{"items": c, "event_types": notify.EventTypes}, err
	}))
	mux.HandleFunc("POST /api/v1/notification-channels", handle(func(r *http.Request, p *auth.Principal) (*notify.Channel, error) {
		in, err := decode[notify.ChannelInput](r)
		if err != nil {
			return nil, err
		}
		return a.Notify.CreateChannel(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/notification-channels/{id}", handle(func(r *http.Request, p *auth.Principal) (*notify.Channel, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[notify.ChannelInput](r)
		if err != nil {
			return nil, err
		}
		return a.Notify.UpdateChannel(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/notification-channels/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Notify.DeleteChannel(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/notification-channels/{id}/test", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Notify.TestChannel(r.Context(), p, id)
	}))

	// Server-Sent Events: live notifications for the signed-in user.
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		rc := http.NewResponseController(w)
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
		w.WriteHeader(http.StatusOK)
		ch, unsubscribe := a.Notify.Hub.Subscribe(p.UserID)
		defer unsubscribe()
		fmt.Fprint(w, "retry: 5000\n\n")
		_ = rc.Flush()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
			case m := <-ch:
				data, _ := json.Marshal(m.Data)
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Event, data); err != nil {
					return
				}
			}
			_ = rc.Flush()
		}
	})
}
