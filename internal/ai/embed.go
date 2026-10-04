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
	chunkChars   = 2000 // about 500 tokens
	chunkOverlap = 300  // about 15 %
	maxChunks    = 200  // per document: bounds work for huge files
)

type chunk struct {
	page int
	text string
}

// chunkPages splits page texts into overlapping chunks that never cross a page, so a
// hit can always be shown with its page number. Cuts prefer sentence or word breaks.
func chunkPages(pages map[int]string) []chunk {
	nums := make([]int, 0, len(pages))
	for n := range pages {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []chunk
	for _, n := range nums {
		text := strings.TrimSpace(pages[n])
		if len([]rune(text)) < 20 {
			continue
		}
		r := []rune(text)
		for start := 0; start < len(r); {
			end := min(start+chunkChars, len(r))
			if end < len(r) {
				// Back up to a sentence end or space within the last 15 % of the window.
				for i := end; i > start+chunkChars*85/100; i-- {
					if r[i-1] == '.' || r[i-1] == '\n' || unicode.IsSpace(r[i-1]) {
						end = i
						break
					}
				}
			}
			out = append(out, chunk{page: n, text: strings.TrimSpace(string(r[start:end]))})
			if end >= len(r) {
				break
			}
			start = max(end-chunkOverlap, start+1)
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
	var title, corr, date string
	var deleted bool
	err := s.pool.QueryRow(ctx, `SELECT d.space_id, d.title, coalesce(c.name,''), coalesce(to_char(d.document_date,'YYYY-MM-DD'),''), d.deleted_at IS NOT NULL
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id WHERE d.id=$1`, docID).Scan(&spaceID, &title, &corr, &date, &deleted)
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
	header := strings.Join(slices.DeleteFunc([]string{title, corr, date}, func(x string) bool { return x == "" }), " | ")
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
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM embeddings WHERE document_id=$1`, docID); err != nil {
			return err
		}
		for i, c := range chunks {
			v := normalize(vecs[i])
			if _, err := tx.Exec(ctx, `INSERT INTO embeddings (document_id, model, chunk_no, page_no, content, vec, dims) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				docID, prov.EmbeddingModel, i, c.page, c.text, encodeVec(v), len(v)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SemHit is the best-matching passage of a document.
type SemHit struct {
	DocumentID uuid.UUID
	Page       int
	Text       string
	Score      float32
}

// Scores below this are noise for typical embedding models; "not found" answers rely on it.
const minSimilarity = 0.30

// scan streams the embeddings of the given spaces and keeps the best chunk per document.
// ponytail: brute force, linear in the number of chunks (fine up to ~50k chunks: a few
// hundred ms). Past that, add pgvector/HNSW behind this one function.
func (s *Service) scan(ctx context.Context, spaceIDs []uuid.UUID, model string, query []float32, exclude uuid.UUID) (map[uuid.UUID]SemHit, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.document_id, e.page_no, e.content, e.vec FROM embeddings e
		JOIN documents d ON d.id=e.document_id WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND e.model=$2 AND d.id<>$3`,
		spaceIDs, model, exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[uuid.UUID]SemHit{}
	for rows.Next() {
		var id uuid.UUID
		var page int
		var text string
		var raw []byte
		if err := rows.Scan(&id, &page, &text, &raw); err != nil {
			return nil, err
		}
		sc := dot(query, decodeVec(raw))
		if cur, ok := best[id]; !ok || sc > cur.Score {
			best[id] = SemHit{DocumentID: id, Page: page, Text: text, Score: sc}
		}
	}
	return best, rows.Err()
}

func top(best map[uuid.UUID]SemHit, limit int, min float32) []SemHit {
	out := make([]SemHit, 0, len(best))
	for _, h := range best {
		if h.Score >= min {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Semantic finds passages by meaning. Only spaces whose AI policy allows the provider are searched.
func (s *Service) Semantic(ctx context.Context, p *auth.Principal, text string, visible []uuid.UUID, limit int) ([]SemHit, error) {
	ids, prov, client, err := s.allowedSpaces(ctx, visible, true)
	if err != nil {
		return nil, err
	}
	vecs, err := client.Embed(ctx, []string{text})
	s.markResult(ctx, prov.ID, err)
	if err != nil {
		return nil, err
	}
	best, err := s.scan(ctx, ids, prov.EmbeddingModel, normalize(vecs[0]), uuid.Nil)
	if err != nil {
		return nil, err
	}
	return top(best, limit, minSimilarity), nil
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
				best, err := s.scan(ctx, spaceIDs, prov.EmbeddingModel, normalize(mean), docID)
				if err != nil {
					return nil, err
				}
				for _, h := range top(best, limit, 0.5) {
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
