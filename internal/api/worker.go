package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/pipeline"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

// Worker protocol v1 (DESIGN §11.3). Authenticated with a worker token.
func (a *API) registerWorker(mux router) {
	worker := func(fn func(w http.ResponseWriter, r *http.Request, p *auth.Principal)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Docveta-Protocol", strconv.Itoa(pipeline.ProtocolVersion))
			p := auth.From(r.Context())
			if p == nil || p.Kind != auth.KindWorker {
				httpx.Error(w, r, apperr.Unauthorized("Worker token required"))
				return
			}
			fn(w, r, p)
		}
	}
	leaseID := func(r *http.Request) (uuid.UUID, error) {
		s := r.URL.Query().Get("lease_id")
		if s == "" {
			s = r.Header.Get("Docveta-Lease")
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return uuid.Nil, apperr.Invalid("lease_id", "lease_id is required")
		}
		return id, nil
	}

	// Enrollment: a worker that can read the enroll key file (shared only with worker
	// containers) gets a token for its name. Disabled unless the key is configured.
	mux.HandleFunc("POST /worker/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docveta-Protocol", strconv.Itoa(pipeline.ProtocolVersion))
		key := a.Cfg.WorkerEnrollKey
		if len(key) == 0 {
			httpx.Error(w, r, apperr.NotFound("Worker enrollment"))
			return
		}
		in, err := decode[struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if subtle.ConstantTimeCompare([]byte(in.Key), key) != 1 {
			time.Sleep(time.Second) // slows down guessing
			httpx.Error(w, r, apperr.Unauthorized("Wrong enrollment key"))
			return
		}
		token, err := a.Pipeline.IssueWorkerToken(r.Context(), in.Name)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"token": token})
	})
	mux.HandleFunc("POST /worker/v1/hello", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		in, err := decode[pipeline.HelloRequest](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		res, err := a.Pipeline.Hello(r.Context(), p, in)
		if err != nil {
			if ae, ok := apperr.As(err); ok && ae.Code == "protocol_unsupported" {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUpgradeRequired)
				_ = json.NewEncoder(w).Encode(httpx.Problem{Title: ae.Msg, Status: 426, Code: ae.Code})
				return
			}
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	}))
	mux.HandleFunc("POST /worker/v1/lease", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		in, err := decode[pipeline.LeaseRequest](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		tasks, err := a.Pipeline.Lease(r.Context(), p, in)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if len(tasks) == 0 {
			httpx.NoContent(w)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"tasks": tasks})
	}))
	mux.HandleFunc("POST /worker/v1/tasks/{id}/heartbeat", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		in, err := decode[pipeline.HeartbeatRequest](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		exp, err := a.Pipeline.Heartbeat(r.Context(), p, id, in)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"lease_expires_at": exp})
	}))
	mux.HandleFunc("GET /worker/v1/tasks/{id}/input", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		lid, err := leaseID(r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		f, _, mime, err := a.Pipeline.OpenTaskInput(r.Context(), p, id, lid)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", mime)
		http.ServeContent(w, r, "", modTimeZero, f)
	}))
	mux.HandleFunc("GET /worker/v1/tasks/{id}/ocr-result", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		lid, err := leaseID(r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		f, _, err := a.Pipeline.OpenTaskOCRResult(r.Context(), p, id, lid)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/json")
		http.ServeContent(w, r, "", modTimeZero, f)
	}))
	mux.HandleFunc("POST /worker/v1/tasks/{id}/complete", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
		mr, err := r.MultipartReader()
		if err != nil {
			httpx.Error(w, r, apperr.Invalid("body", "Send multipart/form-data with lease_id, metrics, result and/or archive parts"))
			return
		}
		in := pipeline.CompleteInput{}
		// Parts are streamed in order: lease_id and metrics first, then files.
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				httpx.Error(w, r, apperr.Invalid("body", "Malformed multipart body"))
				return
			}
			switch part.FormName() {
			case "lease_id":
				b, _ := io.ReadAll(io.LimitReader(part, 100))
				if in.LeaseID, err = uuid.Parse(string(b)); err != nil {
					httpx.Error(w, r, apperr.Invalid("lease_id", "Invalid lease_id"))
					return
				}
			case "metrics":
				_ = json.NewDecoder(io.LimitReader(part, 64<<10)).Decode(&in.Metrics)
			case "result":
				in.Result = part
				err = a.Pipeline.Complete(r.Context(), p, id, in)
				part.Close()
				a.finishComplete(w, r, err)
				return
			case "archive":
				in.Archive = part
				err = a.Pipeline.Complete(r.Context(), p, id, in)
				part.Close()
				a.finishComplete(w, r, err)
				return
			}
			part.Close()
		}
		httpx.Error(w, r, apperr.Invalid("result", "No result or archive part found"))
	}))
	mux.HandleFunc("POST /worker/v1/tasks/{id}/fail", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		in, err := decode[pipeline.FailRequest](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if err := a.Pipeline.Fail(r.Context(), p, id, in); err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.NoContent(w)
	}))
	mux.HandleFunc("POST /worker/v1/tasks/{id}/release", worker(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		in, err := decode[struct {
			LeaseID uuid.UUID `json:"lease_id"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		if err := a.Pipeline.Release(r.Context(), p, id, in.LeaseID); err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.NoContent(w)
	}))
}

func (a *API) finishComplete(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// modTimeZero disables Last-Modified handling for worker downloads.
var modTimeZero time.Time
