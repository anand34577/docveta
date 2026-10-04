package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/ai"
	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/httpx"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/spaces"
)

// listDocuments runs keyword search, or meaning-based / hybrid search when asked for and available.
func (a *API) listDocuments(r *http.Request, p *auth.Principal, q search.Query) (*documents.ListResult, error) {
	if (q.Mode == "semantic" || q.Mode == "hybrid") && q.Q != "" && a.AI != nil {
		return a.AI.Hybrid(r.Context(), a.Documents, p, q)
	}
	q.Mode = ""
	return a.Documents.List(r.Context(), p, q)
}

func (a *API) registerAI(mux router) {
	// --- Providers (admin) ---------------------------------------------------
	mux.HandleFunc("GET /api/v1/ai/status", handle(func(r *http.Request, p *auth.Principal) (*ai.Status, error) {
		return a.AI.Status(r.Context())
	}))
	mux.HandleFunc("GET /api/v1/admin/ai/providers", handle(func(r *http.Request, p *auth.Principal) (list[*ai.Provider], error) {
		l, err := a.AI.ListProviders(r.Context(), p)
		return items(l), err
	}))
	mux.HandleFunc("POST /api/v1/admin/ai/providers", handle(func(r *http.Request, p *auth.Principal) (*ai.Provider, error) {
		in, err := decode[ai.ProviderInput](r)
		if err != nil {
			return nil, err
		}
		return a.AI.CreateProvider(r.Context(), p, in)
	}))
	mux.HandleFunc("PATCH /api/v1/admin/ai/providers/{id}", handleAction(func(r *http.Request, p *auth.Principal) (*ai.Provider, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[ai.ProviderInput](r)
		if err != nil {
			return nil, err
		}
		return a.AI.UpdateProvider(r.Context(), p, id, in)
	}))
	mux.HandleFunc("DELETE /api/v1/admin/ai/providers/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.AI.DeleteProvider(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/admin/ai/providers/{id}/test", handleAction(func(r *http.Request, p *auth.Principal) (*ai.TestResult, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.AI.TestProvider(r.Context(), p, id)
	}))
	mux.HandleFunc("POST /api/v1/admin/ai/reindex", handleAction(func(r *http.Request, p *auth.Principal) (map[string]int, error) {
		ids, err := a.AI.MissingEmbeddings(r.Context(), p)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if err := a.Queue.Insert(r.Context(), jobs.AIArgs{DocumentID: id, Embed: true}, &river.InsertOpts{Priority: jobs.PriorityBackground, MaxAttempts: 2}); err != nil {
				return nil, err
			}
		}
		return map[string]int{"queued": len(ids)}, nil
	}))

	// --- Suggestions ---------------------------------------------------------
	mux.HandleFunc("GET /api/v1/documents/{id}/suggestions", handle(func(r *http.Request, p *auth.Principal) (list[ai.Suggestion], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[ai.Suggestion]{}, err
		}
		l, err := a.AI.Pending(r.Context(), a.Documents, p, id)
		return items(l), err
	}))
	resolve := func(accept bool) http.HandlerFunc {
		return handleNoContent(func(r *http.Request, p *auth.Principal) error {
			id, err := httpx.PathUUID(r, "id")
			if err != nil {
				return err
			}
			in, err := decode[struct {
				IDs []uuid.UUID `json:"ids"` // omitted = every open suggestion
			}](r)
			if err != nil {
				return err
			}
			return a.AI.Resolve(r.Context(), a.Documents, p, id, in.IDs, accept)
		})
	}
	mux.HandleFunc("POST /api/v1/documents/{id}/suggestions/accept", resolve(true))
	mux.HandleFunc("POST /api/v1/documents/{id}/suggestions/reject", resolve(false))
	mux.HandleFunc("POST /api/v1/documents/{id}/ai", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		acc, err := a.Documents.Access(r.Context(), p, id, spaces.ActEdit)
		if err != nil {
			return err
		}
		if acc.Deleted {
			return apperr.Conflict("in_trash", "Restore this document first")
		}
		return a.Queue.Insert(r.Context(), jobs.AIArgs{DocumentID: id, Classify: true, Embed: true},
			&river.InsertOpts{Priority: jobs.PriorityInteractive, MaxAttempts: 2})
	}))
	mux.HandleFunc("GET /api/v1/documents/{id}/similar", handle(func(r *http.Request, p *auth.Principal) (list[ai.SimilarDoc], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[ai.SimilarDoc]{}, err
		}
		l, err := a.AI.Similar(r.Context(), a.Documents, p, id, httpx.QueryInt(r, "limit", 8, 1, 20))
		return items(l), err
	}))
	mux.HandleFunc("GET /api/v1/spaces/{id}/ai-stats", handle(func(r *http.Request, p *auth.Principal) (*ai.SpaceStats, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		return a.AI.Stats(r.Context(), p, id)
	}))

	// --- Ask your documents ----------------------------------------------------
	mux.HandleFunc("POST /api/v1/ai/ask", func(w http.ResponseWriter, r *http.Request) {
		p := user(w, r)
		if p == nil {
			return
		}
		in, err := decode[ai.AskInput](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		rc := http.NewResponseController(w)
		var mu sync.Mutex // the answer and the keep-alive write from different goroutines
		started := false
		start := func() {
			if started {
				return
			}
			started = true
			h := w.Header()
			h.Set("Content-Type", "text/event-stream")
			h.Set("Cache-Control", "no-cache")
			h.Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
		}
		emit := func(event string, data any) {
			mu.Lock()
			defer mu.Unlock()
			start()
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
			_ = rc.Flush()
		}
		// A model that is still loading can take a minute to say anything; a comment line every
		// few seconds keeps proxies (nginx, Cloudflare) from closing the quiet connection.
		stopPing := make(chan struct{})
		defer close(stopPing)
		go func() {
			t := time.NewTicker(15 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stopPing:
					return
				case <-r.Context().Done():
					return
				case <-t.C:
					mu.Lock()
					if started {
						_, _ = fmt.Fprint(w, ": keep-alive\n\n")
						_ = rc.Flush()
					}
					mu.Unlock()
				}
			}
		}()
		// Errors before the first event are normal problem responses; later ones are events.
		if err := a.AI.Ask(r.Context(), p, in, emit); err != nil {
			if r.Context().Err() != nil {
				return // the person stopped the answer or left
			}
			mu.Lock()
			wasStarted := started
			mu.Unlock()
			if !wasStarted {
				httpx.Error(w, r, err)
				return
			}
			msg := "Something went wrong while answering."
			if ae, ok := apperr.As(err); ok && ae.Kind != apperr.KindInternal {
				msg = ae.Msg
			} else {
				httpx.Logger(r.Context()).Error("ask failed", "err", err)
			}
			emit("error", map[string]string{"message": msg})
		}
	})
	mux.HandleFunc("GET /api/v1/ai/conversations", handle(func(r *http.Request, p *auth.Principal) (list[ai.Conversation], error) {
		var before *time.Time
		if v := r.URL.Query().Get("before"); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				return list[ai.Conversation]{}, apperr.Invalid("before", "Use an RFC 3339 time")
			}
			before = &t
		}
		c, err := a.AI.Conversations(r.Context(), p, before, httpx.QueryInt(r, "limit", 50, 1, 200))
		return items(c), err
	}))
	mux.HandleFunc("DELETE /api/v1/ai/conversations", handle(func(r *http.Request, p *auth.Principal) (map[string]int64, error) {
		n, err := a.AI.DeleteAllConversations(r.Context(), p)
		return map[string]int64{"deleted": n}, err
	}))
	mux.HandleFunc("GET /api/v1/ai/conversations/{id}/messages", handle(func(r *http.Request, p *auth.Principal) (list[ai.ConversationMessage], error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return list[ai.ConversationMessage]{}, err
		}
		m, err := a.AI.Messages(r.Context(), p, id)
		return items(m), err
	}))
	mux.HandleFunc("PATCH /api/v1/ai/conversations/{id}", handle(func(r *http.Request, p *auth.Principal) (*ai.Conversation, error) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return nil, err
		}
		in, err := decode[struct {
			Title string `json:"title"`
		}](r)
		if err != nil {
			return nil, err
		}
		return a.AI.RenameConversation(r.Context(), p, id, in.Title)
	}))
	mux.HandleFunc("DELETE /api/v1/ai/conversations/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.AI.DeleteConversation(r.Context(), p, id)
	}))
}
