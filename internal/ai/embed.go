package ai

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/spaces"
)

const (
	// Chunks are measured in estimated tokens, not characters: embedding models have token
	// limits (often 512), and one Devanagari or Tamil character costs about one token where
	// four English letters do.
	chunkTokens   = 380
	overlapTokens = 60  // about 15 %
	maxChunks     = 400 // per document: bounds work for huge files
)

type chunk struct {
	page int
	text string
}

// tokenCost estimates how many model tokens a character uses.
func tokenCost(r rune) float64 {
	switch {
	case r < 0x80:
		return 0.25
	case r < 0x300: // accented Latin
		return 0.5
	}
	return 1
}

// Places to end a chunk, best first: a line break, a sentence end (including the Devanagari
// danda), any space.
var chunkBreaks = []func(rune) bool{
	func(c rune) bool { return c == '\n' },
	func(c rune) bool { return c == '.' || c == '?' || c == '!' || c == '।' },
	unicode.IsSpace,
}

// chunkPages splits page texts into overlapping chunks that never cross a page, so a
// hit can always be shown with its page number. Cuts prefer line, sentence or word breaks.
func chunkPages(pages map[int]string) []chunk {
	nums := make([]int, 0, len(pages))
	for n := range pages {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []chunk
	for _, n := range nums {
		r := []rune(strings.TrimSpace(pages[n]))
		if len(r) < 20 {
			continue
		}
		for start := 0; start < len(r); {
			// Grow the window to the token budget.
			end, cost := start, 0.0
			for end < len(r) && cost+tokenCost(r[end]) <= chunkTokens {
				cost += tokenCost(r[end])
				end++
			}
			if end < len(r) {
				// Back up to a break within the last 15 % of the window.
				floor := start + (end-start)*85/100
			breaks:
				for _, isBreak := range chunkBreaks {
					for i := end; i > floor; i-- {
						if isBreak(r[i-1]) {
							end = i
							break breaks
						}
					}
				}
			}
			if t := strings.TrimSpace(string(r[start:end])); t != "" {
				out = append(out, chunk{page: n, text: t})
			}
			if end >= len(r) {
				break
			}
			// Start the next chunk about overlapTokens earlier.
			back, c := end, 0.0
			for back > start+1 && c < overlapTokens {
				back--
				c += tokenCost(r[back])
			}
			start = max(back, start+1)
		}
	}
	return out
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(sum))
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func dot(a, b []float32) float32 {
	if len(a) != len(b) {
		return -1
	}
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// EmbedDocument (re)builds a document's embeddings. Quiet no-op when AI is off or the
// provider has no embedding model.
func (s *Service) EmbedDocument(ctx context.Context, docID uuid.UUID) error {
	var spaceID uuid.UUID
	var title, corr, typ, date string
	var deleted bool
	err := s.pool.QueryRow(ctx, `SELECT d.space_id, d.title, coalesce(c.name,''), coalesce(t.name,''), coalesce(to_char(d.document_date,'YYYY-MM-DD'),''),
			d.deleted_at IS NOT NULL
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id LEFT JOIN document_types t ON t.id=d.document_type_id
		WHERE d.id=$1`, docID).Scan(&spaceID, &title, &corr, &typ, &date, &deleted)
	if db.IsNoRows(err) || deleted {
		return nil
	}
	if err != nil {
		return err
	}
	prov, client, err := s.forSpace(ctx, spaceID, true)
	if err != nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT page_no, text FROM pages WHERE document_id=$1 ORDER BY page_no`, docID)
	if err != nil {
		return err
	}
	pages := map[int]string{}
	for rows.Next() {
		var n int
		var t string
		if err := rows.Scan(&n, &t); err != nil {
			rows.Close()
			return err
		}
		pages[n] = t
	}
	rows.Close()
	chunks := chunkPages(pages)
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}
	// The document's details go with every chunk, so "my Tata Power bill" finds its pages.
	header := strings.Join(slices.DeleteFunc([]string{title, corr, typ, date}, func(x string) bool { return x == "" }), " | ")
	inputs := make([]string, len(chunks))
	for i, c := range chunks {
		inputs[i] = header + "\n" + c.text
	}
	var vecs [][]float32
	if len(inputs) > 0 {
		vecs, err = client.Embed(ctx, inputs)
		s.markResult(ctx, prov.ID, err)
		if err != nil {
			return fmt.Errorf("embed document: %w", err)
		}
	}
	pgv, _ := s.pgvector(ctx)
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM embeddings WHERE document_id=$1`, docID); err != nil {
			return err
		}
		b := &pgx.Batch{}
		for i, c := range chunks {
			v := normalize(vecs[i])
			if len(v) == 0 {
				return fmt.Errorf("the AI server returned an empty embedding")
			}
			if pgv {
				b.Queue(`INSERT INTO embeddings (document_id, model, chunk_no, page_no, content, vec, dims, embedding) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::vector)`,
					docID, prov.EmbeddingModel, i, c.page, c.text, encodeVec(v), len(v), vecLiteral(v))
			} else {
				b.Queue(`INSERT INTO embeddings (document_id, model, chunk_no, page_no, content, vec, dims) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
					docID, prov.EmbeddingModel, i, c.page, c.text, encodeVec(v), len(v))
			}
		}
		if b.Len() == 0 {
			return nil
		}
		return tx.SendBatch(ctx, b).Close()
	})
}

// SemHit is a passage that matches by meaning.
type SemHit struct {
	DocumentID uuid.UUID
	Page       int
	Text       string
	Score      float32
}

// Scores below this are noise for typical embedding models; "not found" answers rely on it.
const minSimilarity = 0.30

// topK keeps the k best hits seen so far (a sorted slice: k is at most a few hundred).
type topK struct {
	k    int
	hits []SemHit
}

func newTopK(k int) *topK { return &topK{k: max(k, 1)} }

func (t *topK) add(h SemHit) {
	if len(t.hits) == t.k && h.Score <= t.hits[len(t.hits)-1].Score {
		return
	}
	i := sort.Search(len(t.hits), func(i int) bool { return t.hits[i].Score < h.Score })
	t.hits = slices.Insert(t.hits, i, h)
	if len(t.hits) > t.k {
		t.hits = t.hits[:t.k]
	}
}

func (t *topK) sorted() []SemHit { return t.hits }

func sortHits(h []SemHit) {
	sort.SliceStable(h, func(i, j int) bool { return h[i].Score > h[j].Score })
}

// bestPerDocument keeps each document's best passage, in score order, at or above min.
func bestPerDocument(hits []SemHit, limit int, min float32) []SemHit {
	seen := map[uuid.UUID]bool{}
	var out []SemHit
	for _, h := range hits {
		if h.Score < min || seen[h.DocumentID] {
			continue
		}
		seen[h.DocumentID] = true
		out = append(out, h)
		if len(out) == limit {
			break
		}
	}
	return out
}

// embedQuery turns search text into a normalised vector with the provider's embedding model.
func (s *Service) embedQuery(ctx context.Context, prov *Provider, client *Client, text string) ([]float32, error) {
	vecs, err := client.Embed(ctx, []string{text})
	s.markResult(ctx, prov.ID, err)
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("the AI server returned an empty embedding")
	}
	return normalize(vecs[0]), nil
}

// Semantic finds documents by meaning (their best passage each). Only spaces whose AI policy
// allows the provider are searched.
func (s *Service) Semantic(ctx context.Context, p *auth.Principal, text string, visible []uuid.UUID, limit int) ([]SemHit, error) {
	ids, prov, client, err := s.allowedSpaces(ctx, visible, true)
	if err != nil {
		return nil, err
	}
	q, err := s.embedQuery(ctx, prov, client, text)
	if err != nil {
		return nil, err
	}
	// Several passages per document come back; ask for enough to fill `limit` documents.
	hits, err := s.searchChunks(ctx, chunkFilter{Spaces: ids, Model: prov.EmbeddingModel}, q, min(limit*4, 800))
	if err != nil {
		return nil, err
	}
	return bestPerDocument(hits, limit, minSimilarity), nil
}

// Passages finds the passages closest in meaning to text, at most perDoc from one document.
// docs limits the search to those documents (asking about specific documents).
func (s *Service) Passages(ctx context.Context, text string, spaceIDs, docs []uuid.UUID, limit, perDoc int) ([]SemHit, error) {
	ids, prov, client, err := s.allowedSpaces(ctx, spaceIDs, true)
	if err != nil {
		return nil, err
	}
	q, err := s.embedQuery(ctx, prov, client, text)
	if err != nil {
		return nil, err
	}
	hits, err := s.searchChunks(ctx, chunkFilter{Spaces: ids, Docs: docs, Model: prov.EmbeddingModel}, q, limit*3)
	if err != nil {
		return nil, err
	}
	return capPerDocument(hits, limit, perDoc, minSimilarity), nil
}

// capPerDocument keeps hits in order, at most perDoc per document and limit in all.
func capPerDocument(hits []SemHit, limit, perDoc int, min float32) []SemHit {
	count := map[uuid.UUID]int{}
	var out []SemHit
	for _, h := range hits {
		if h.Score < min || count[h.DocumentID] >= perDoc {
			continue
		}
		count[h.DocumentID]++
		out = append(out, h)
		if len(out) == limit {
			break
		}
	}
	return out
}

// SimilarDoc is a document related to another.
type SimilarDoc struct {
	Document *documents.Document `json:"document"`
	Score    float32             `json:"score"`
	Reason   string              `json:"reason"` // meaning | details
}

// Similar returns documents like the given one: by meaning when it has embeddings,
// otherwise by shared correspondent, type and tags.
func (s *Service) Similar(ctx context.Context, docs *documents.Service, p *auth.Principal, docID uuid.UUID, limit int) ([]SimilarDoc, error) {
	a, err := docs.Access(ctx, p, docID, spaces.ActView)
	if err != nil {
		return nil, err
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	scores := map[uuid.UUID]float32{}
	reason := "details"
	if allowed, prov, _, err := s.allowedSpaces(ctx, []uuid.UUID{a.SpaceID}, true); err == nil && len(allowed) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT vec FROM embeddings WHERE document_id=$1 AND model=$2`, docID, prov.EmbeddingModel)
		if err != nil {
			return nil, err
		}
		var mean []float32
		n := 0
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				rows.Close()
				return nil, err
			}
			v := decodeVec(raw)
			if mean == nil {
				mean = make([]float32, len(v))
			}
			if len(v) == len(mean) {
				for i := range v {
					mean[i] += v[i]
				}
				n++
			}
		}
		rows.Close()
		if n > 0 {
			spaceIDs, _, _, aerr := s.allowedSpaces(ctx, visible, true)
			if aerr == nil {
				hits, err := s.searchChunks(ctx, chunkFilter{Spaces: spaceIDs, Exclude: docID, Model: prov.EmbeddingModel}, normalize(mean), limit*6)
				if err != nil {
					return nil, err
				}
				for _, h := range bestPerDocument(hits, limit, 0.5) {
					ids = append(ids, h.DocumentID)
					scores[h.DocumentID] = h.Score
				}
				reason = "meaning"
			}
		}
	}
	if len(ids) == 0 {
		rows, err := s.pool.Query(ctx, `SELECT d.id, (
				(CASE WHEN d.correspondent_id IS NOT NULL AND d.correspondent_id = me.correspondent_id THEN 2 ELSE 0 END) +
				(CASE WHEN d.document_type_id IS NOT NULL AND d.document_type_id = me.document_type_id THEN 1 ELSE 0 END) +
				(SELECT count(*) FROM document_tags a JOIN document_tags b ON a.tag_id=b.tag_id WHERE a.document_id=d.id AND b.document_id=me.id)
			)::float4 AS score
			FROM documents d, documents me WHERE me.id=$1 AND d.id<>me.id AND d.space_id = ANY($2) AND d.deleted_at IS NULL
			  AND (d.correspondent_id = me.correspondent_id OR d.document_type_id = me.document_type_id
			       OR EXISTS (SELECT 1 FROM document_tags a JOIN document_tags b ON a.tag_id=b.tag_id WHERE a.document_id=d.id AND b.document_id=me.id))
			ORDER BY score DESC, d.added_at DESC LIMIT $3`, docID, visible, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var sc float32
			if err := rows.Scan(&id, &sc); err != nil {
				rows.Close()
				return nil, err
			}
			if sc > 0 {
				ids = append(ids, id)
				scores[id] = sc
			}
		}
		rows.Close()
	}
	hydrated, err := documents.Hydrate(ctx, s.pool, ids)
	if err != nil {
		return nil, err
	}
	out := make([]SimilarDoc, 0, len(hydrated))
	for _, d := range hydrated {
		out = append(out, SimilarDoc{Document: d, Score: scores[d.ID], Reason: reason})
	}
	return out, nil
}

// MissingEmbeddings returns ids of ready documents without embeddings for the current
// model (back-fill after turning the feature on or switching models).
func (s *Service) MissingEmbeddings(ctx context.Context, p *auth.Principal) ([]uuid.UUID, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	var model string
	if err := s.pool.QueryRow(ctx, `SELECT coalesce((SELECT embedding_model FROM ai_providers WHERE enabled ORDER BY is_default DESC, created_at LIMIT 1), '')`).Scan(&model); err != nil || model == "" {
		return nil, ErrNoEmbeds
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id FROM documents d JOIN spaces sp ON sp.id=d.space_id
		WHERE d.deleted_at IS NULL AND d.status='ready' AND sp.ai_policy <> 'off'
		  AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.document_id=d.id AND e.model=$1)`, model)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
