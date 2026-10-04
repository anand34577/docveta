package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// pgvector support. When the extension is installed, every chunk's vector is also stored in
// embeddings.embedding and searched with HNSW indexes (one per vector size, because models
// differ). Otherwise vectors are scanned in Docveta (fine for a few thousand documents).

type vectorState struct {
	mu        sync.Mutex
	checked   time.Time
	ok        bool
	iterative bool // pgvector ≥ 0.8: filtered HNSW searches keep looking until they have enough rows
	indexes   map[int]bool
}

// hnswMax: HNSW indexes handle up to 2000 dimensions as vector and 4000 as halfvec.
const hnswVectorMax, hnswHalfMax = 2000, 4000

// pgvector reports whether the database can search vectors itself. Checked every few minutes,
// so installing pgvector later takes effect without a restart.
func (s *Service) pgvector(ctx context.Context) (ok, iterative bool) {
	v := &s.vec
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.checked) < 5*time.Minute {
		return v.ok, v.iterative
	}
	v.checked = time.Now()
	var version string
	if err := s.pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname='vector'`).Scan(&version); err != nil {
		v.ok = false
		return false, false
	}
	var col bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='embeddings' AND column_name='embedding')`).Scan(&col)
	if !col {
		// The extension arrived after the migration ran: add the column now.
		if _, err := s.pool.Exec(ctx, `ALTER TABLE embeddings ADD COLUMN IF NOT EXISTS embedding vector`); err != nil {
			slog.Warn("pgvector is installed but the embeddings table can't use it", "err", err)
			v.ok = false
			return false, false
		}
	}
	v.ok, v.iterative = true, versionAtLeast(version, 0, 8)
	if v.indexes == nil {
		v.indexes = map[int]bool{}
	}
	return v.ok, v.iterative
}

func versionAtLeast(v string, major, minor int) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	a, _ := strconv.Atoi(parts[0])
	b, _ := strconv.Atoi(parts[1])
	return a > major || (a == major && b >= minor)
}

// vecType is the type a vector of n dimensions is indexed and compared as.
func vecType(n int) string {
	if n > hnswVectorMax && n <= hnswHalfMax {
		return fmt.Sprintf("halfvec(%d)", n)
	}
	return fmt.Sprintf("vector(%d)", n)
}

// ensureIndex builds the HNSW index for vectors of n dimensions in the background, once.
// Searches work (more slowly) while it is being built.
func (s *Service) ensureIndex(n int) {
	if n <= 0 || n > hnswHalfMax {
		return // too large to index: searched without an index
	}
	v := &s.vec
	v.mu.Lock()
	if v.indexes == nil {
		v.indexes = map[int]bool{}
	}
	if v.indexes[n] {
		v.mu.Unlock()
		return
	}
	v.indexes[n] = true
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		ops := "vector_cosine_ops"
		if n > hnswVectorMax {
			ops = "halfvec_cosine_ops"
		}
		// CONCURRENTLY: documents can still be added while a large index is built.
		sql := fmt.Sprintf(`CREATE INDEX CONCURRENTLY IF NOT EXISTS embeddings_hnsw_%d ON embeddings
			USING hnsw ((embedding::%s) %s) WHERE dims = %d`, n, vecType(n), ops, n)
		if _, err := s.pool.Exec(ctx, sql); err != nil {
			slog.Warn("couldn't build the vector index", "dims", n, "err", err)
			v.mu.Lock()
			v.indexes[n] = false // try again later
			v.mu.Unlock()
		}
	}()
}

// vecLiteral formats a vector for pgvector ('[0.1,0.2,…]').
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', 7, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// chunkFilter limits a vector search.
type chunkFilter struct {
	Spaces  []uuid.UUID
	Docs    []uuid.UUID // only these documents (empty = all in Spaces)
	Exclude uuid.UUID
	Model   string
}

// searchChunks returns the k passages closest to query, best first.
func (s *Service) searchChunks(ctx context.Context, f chunkFilter, query []float32, k int) ([]SemHit, error) {
	if ok, iterative := s.pgvector(ctx); ok {
		hits, err := s.searchChunksPG(ctx, f, query, k, iterative)
		if err == nil {
			return hits, nil
		}
		slog.Warn("vector search in the database failed; scanning instead", "err", err)
	}
	return s.scanChunks(ctx, f, query, k)
}

func (s *Service) searchChunksPG(ctx context.Context, f chunkFilter, query []float32, k int, iterative bool) ([]SemHit, error) {
	n := len(query)
	s.ensureIndex(n)
	t := vecType(n)
	var out []SemHit
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Look further than k neighbours: filters (spaces, trash) drop some of them.
		if _, err := tx.Exec(ctx, `SET LOCAL hnsw.ef_search = `+strconv.Itoa(min(max(k*4, 100), 1000))); err != nil {
			return err
		}
		if iterative {
			if _, err := tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan = relaxed_order`); err != nil {
				return err
			}
		}
		// dims is written into the SQL: the partial HNSW index (WHERE dims = n) only matches a literal.
		sql := `SELECT e.document_id, e.page_no, e.content, 1 - ((e.embedding::` + t + `) <=> $1::` + t + `) AS score
			FROM embeddings e JOIN documents d ON d.id = e.document_id
			WHERE e.dims = ` + strconv.Itoa(n) + ` AND e.model = $2 AND e.embedding IS NOT NULL AND d.space_id = ANY($3) AND d.deleted_at IS NULL
			  AND d.id <> $4 AND (cardinality($5::uuid[]) = 0 OR d.id = ANY($5))
			ORDER BY (e.embedding::` + t + `) <=> $1::` + t + ` LIMIT $6`
		docs := f.Docs
		if docs == nil {
			docs = []uuid.UUID{}
		}
		rows, err := tx.Query(ctx, sql, vecLiteral(query), f.Model, f.Spaces, f.Exclude, docs, k)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h SemHit
			var sc float64
			if err := rows.Scan(&h.DocumentID, &h.Page, &h.Text, &sc); err != nil {
				return err
			}
			h.Score = float32(sc)
			out = append(out, h)
		}
		return rows.Err()
	})
	sortHits(out) // relaxed_order may return neighbours slightly out of order
	return out, err
}

// scanChunks is the fallback without pgvector: it streams the stored vectors and keeps the
// best k (memory stays bounded by k, time grows with the number of chunks).
func (s *Service) scanChunks(ctx context.Context, f chunkFilter, query []float32, k int) ([]SemHit, error) {
	docs := f.Docs
	if docs == nil {
		docs = []uuid.UUID{}
	}
	rows, err := s.pool.Query(ctx, `SELECT e.document_id, e.page_no, e.content, e.vec FROM embeddings e
		JOIN documents d ON d.id=e.document_id
		WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND e.model=$2 AND e.dims=$3 AND d.id<>$4
		  AND (cardinality($5::uuid[]) = 0 OR d.id = ANY($5))`, f.Spaces, f.Model, len(query), f.Exclude, docs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := newTopK(k)
	for rows.Next() {
		var h SemHit
		var raw []byte
		if err := rows.Scan(&h.DocumentID, &h.Page, &h.Text, &raw); err != nil {
			return nil, err
		}
		h.Score = dot(query, decodeVec(raw))
		best.add(h)
	}
	return best.sorted(), rows.Err()
}

// BackfillVectors copies vectors stored before pgvector was available into the vector
// column, in batches. Returns how many it copied.
func (s *Service) BackfillVectors(ctx context.Context, max int) (int, error) {
	if ok, _ := s.pgvector(ctx); !ok {
		return 0, nil
	}
	done := 0
	for done < max {
		rows, err := s.pool.Query(ctx, `SELECT document_id, model, chunk_no, vec FROM embeddings WHERE embedding IS NULL LIMIT 200`)
		if err != nil {
			return done, err
		}
		type item struct {
			doc   uuid.UUID
			model string
			chunk int
			vec   []byte
		}
		var batch []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.doc, &it.model, &it.chunk, &it.vec); err != nil {
				rows.Close()
				return done, err
			}
			batch = append(batch, it)
		}
		rows.Close()
		if len(batch) == 0 {
			return done, nil
		}
		b := &pgx.Batch{}
		for _, it := range batch {
			b.Queue(`UPDATE embeddings SET embedding=$4::vector WHERE document_id=$1 AND model=$2 AND chunk_no=$3`,
				it.doc, it.model, it.chunk, vecLiteral(decodeVec(it.vec)))
		}
		if err := s.pool.SendBatch(ctx, b).Close(); err != nil {
			return done, err
		}
		done += len(batch)
	}
	return done, nil
}
