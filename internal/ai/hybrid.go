package ai

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/search"
	"github.com/anand34577/docveta/internal/textindex"
)

// Worker runs the AI steps for a document in the background. Failures are logged, not
// retried hard: the document is already usable.
type Worker struct {
	river.WorkerDefaults[jobs.AIArgs]
	S *Service
}

func (w *Worker) Timeout(*river.Job[jobs.AIArgs]) time.Duration { return 15 * time.Minute }

func (w *Worker) Work(ctx context.Context, job *river.Job[jobs.AIArgs]) error {
	a := job.Args
	if a.Classify {
		if err := w.S.ClassifyDocument(ctx, a.DocumentID); err != nil {
			slog.Warn("AI classification error", "document", a.DocumentID, "err", err)
		}
	}
	if a.Embed {
		if err := w.S.EmbedDocument(ctx, a.DocumentID); err != nil {
			slog.Warn("AI embedding error", "document", a.DocumentID, "err", err)
		}
	}
	return nil
}

const rrfK = 60

// Hybrid searches by meaning and/or keywords. Mode "semantic" returns only meaning-based
// results; "hybrid" fuses them with the keyword results (reciprocal rank fusion). Filters
// (tags, dates, spaces, ...) apply to both. When meaning-based search isn't available it
// quietly falls back to keywords and says so in Mode.
func (s *Service) Hybrid(ctx context.Context, docs *documents.Service, p *auth.Principal, q search.Query) (*documents.ListResult, error) {
	keyword := func() (*documents.ListResult, error) {
		q.Mode = ""
		res, err := docs.List(ctx, p, q)
		if res != nil {
			res.Mode = "keyword"
		}
		return res, err
	}
	parsed := search.Parse(q.Q, time.Now())
	text := strings.TrimSpace(parsed.Raw)
	if text == "" {
		return keyword()
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if len(q.SpaceIDs) > 0 {
		var keep []uuid.UUID
		for _, id := range q.SpaceIDs {
			for _, v := range visible {
				if v == id {
					keep = append(keep, id)
				}
			}
		}
		visible = keep
	}
	hits, err := s.Semantic(ctx, p, text, visible, 100)
	if err != nil {
		return keyword()
	}
	hitByDoc := map[uuid.UUID]SemHit{}
	ids := make([]uuid.UUID, 0, len(hits))
	for _, h := range hits {
		hitByDoc[h.DocumentID] = h
		ids = append(ids, h.DocumentID)
	}

	// Semantic leg: the same filters, restricted to the passages' documents.
	qs := q
	qs.Q, qs.IDs, qs.Limit, qs.Cursor, qs.Mode, qs.Sort = strings.Join(parsed.FilterTokens, " "), ids, 200, "", "", "-added"
	if len(ids) == 0 {
		qs.IDs = []uuid.UUID{uuid.Nil}
	}
	semRes, err := docs.List(ctx, p, qs)
	if err != nil {
		return nil, err
	}
	allowed := map[uuid.UUID]*documents.Document{}
	for _, d := range semRes.Items {
		allowed[d.ID] = d
	}
	score := map[uuid.UUID]float64{}
	byID := map[uuid.UUID]*documents.Document{}
	rank := 0
	for _, h := range hits { // hits are already best-first
		if d, ok := allowed[h.DocumentID]; ok {
			score[d.ID] += 1 / float64(rrfK+rank)
			byID[d.ID] = d
			rank++
		}
	}
	mode := "semantic"
	if q.Mode != "semantic" {
		mode = "hybrid"
		qk := q
		qk.Limit, qk.Cursor, qk.Mode, qk.Sort = 100, "", "", ""
		kw, err := docs.List(ctx, p, qk)
		if err != nil {
			return nil, err
		}
		for i, d := range kw.Items {
			score[d.ID] += 1 / float64(rrfK+i)
			byID[d.ID] = d
		}
	}
	out := make([]*documents.Document, 0, len(byID))
	for _, d := range byID {
		out = append(out, d)
	}
	sortByScore(out, score)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	for _, d := range out {
		if len(d.Snippet) == 0 {
			if h, ok := hitByDoc[d.ID]; ok {
				pg := h.Page
				d.MatchedPage = &pg
				d.Snippet = []textindex.Segment{{Text: snippetOf(h.Text)}}
			}
		}
	}
	total := len(byID)
	return &documents.ListResult{Mode: mode, Items: out, Total: &total}, nil
}

func sortByScore(docs []*documents.Document, score map[uuid.UUID]float64) {
	for i := 1; i < len(docs); i++ {
		for j := i; j > 0 && score[docs[j].ID] > score[docs[j-1].ID]; j-- {
			docs[j], docs[j-1] = docs[j-1], docs[j]
		}
	}
}
