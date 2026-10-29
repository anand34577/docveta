package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/spaces"
	"github.com/anand34577/docveta/internal/textindex"
)

// Query combines the free-text query (with inline filters) and structured filters
// coming from UI chips / API parameters. Both are ANDed.
type Query struct {
	Q                string         `json:"q,omitempty"`
	SpaceIDs         []uuid.UUID    `json:"space_ids,omitempty"`
	TagIDs           []uuid.UUID    `json:"tag_ids,omitempty"`     // documents must have all
	AnyTagIDs        []uuid.UUID    `json:"any_tag_ids,omitempty"` // documents must have at least one
	NotTagIDs        []uuid.UUID    `json:"not_tag_ids,omitempty"`
	CorrespondentIDs []uuid.UUID    `json:"correspondent_ids,omitempty"`
	TypeIDs          []uuid.UUID    `json:"type_ids,omitempty"`
	DateFrom         *string        `json:"date_from,omitempty"`
	DateTo           *string        `json:"date_to,omitempty"`
	AddedFrom        *time.Time     `json:"added_from,omitempty"`
	AddedTo          *time.Time     `json:"added_to,omitempty"`
	Inbox            *bool          `json:"inbox,omitempty"`
	Statuses         []string       `json:"statuses,omitempty"`
	Untagged         bool           `json:"untagged,omitempty"`
	Custom           []CustomFilter `json:"custom,omitempty"` // custom field comparisons, ANDed
	Mode             string         `json:"mode,omitempty"`   // keyword (default) | semantic | hybrid
	IDs              []uuid.UUID    `json:"-"`                // restrict to these documents (used by meaning-based search)
	NoCorrespondent  bool           `json:"no_correspondent,omitempty"`
	NoType           bool           `json:"no_type,omitempty"`
	CollectionID     *uuid.UUID     `json:"collection_id,omitempty"` // in this collection (the caller checks they can see it)
	Trash            bool           `json:"trash,omitempty"`
	Sort             string         `json:"sort,omitempty"` // relevance|added|-added|date|-date|title|-title|updated|cf:<field id> (prefix - for descending)
	Cursor           string         `json:"cursor,omitempty"`
	Limit            int            `json:"limit,omitempty"`
	WithTotal        bool           `json:"-"`
	WithFacets       bool           `json:"-"` // also count matching documents per tag, correspondent, type and status
}

type Hit struct {
	ID          uuid.UUID           `json:"id"`
	Rank        float64             `json:"rank,omitempty"`
	Snippet     []textindex.Segment `json:"snippet,omitempty"`
	MatchedPage *int                `json:"matched_page,omitempty"`
}

type Result struct {
	Hits  []Hit `json:"hits"`
	Total *int  `json:"total,omitempty"`
	// TotalCapped: there are more than Total matches (counting stopped at TotalCap).
	TotalCapped bool    `json:"total_capped,omitempty"`
	NextCursor  *string `json:"next_cursor"`
	Facets      *Facets `json:"facets,omitempty"`
}

// Facets are document counts under the current filters, keyed by the id (or status name).
type Facets struct {
	Tags           map[string]int `json:"tags"`
	Correspondents map[string]int `json:"correspondents"`
	Types          map[string]int `json:"types"`
	Statuses       map[string]int `json:"statuses"`
}

type Service struct {
	pool   *pgxpool.Pool
	spaces *spaces.Service
	now    func() time.Time
}

func NewService(pool *pgxpool.Pool, sp *spaces.Service) *Service {
	return &Service{pool: pool, spaces: sp, now: time.Now}
}

// builder accumulates SQL conditions with positional args.
type builder struct {
	conds []string
	args  []any
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *builder) where(cond string) { b.conds = append(b.conds, cond) }

// addedKeyLayout is how the date added travels in a cursor (to_char's YYYY-MM-DD"T"HH24:MI:SS.US, in UTC).
const addedKeyLayout = "2006-01-02T15:04:05.000000"

type cursor struct {
	Offset int     `json:"o,omitempty"`
	Key    *string `json:"k,omitempty"`
	ID     string  `json:"i,omitempty"`
	Broad  bool    `json:"b,omitempty"` // best match among the newest matches (see broadMatch), kept for every page
}

func encodeCursor(c cursor) *string {
	raw, _ := json.Marshal(c)
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	if err == nil && c.Key != nil {
		_, err = uuid.Parse(c.ID) // a keyset cursor's tie-breaker goes to the database as a uuid
	}
	if err == nil && c.Offset < 0 {
		err = fmt.Errorf("negative offset")
	}
	if err != nil {
		return c, apperr.Invalid("cursor", "Invalid cursor")
	}
	return c, nil
}

// cursorKey turns a keyset cursor's key into the value its sort compares against, so a
// damaged key is a bad request rather than a database error.
func cursorKey(sort, key string) (any, error) {
	switch sort {
	case "added", "updated":
		if at, err := time.Parse(addedKeyLayout, key); err == nil {
			return at, nil
		}
	case "date":
		if key == "-infinity" || key == "0000-00-00" { // 0000-00-00: cursors from before 0.18
			return pgtype.Date{InfinityModifier: pgtype.NegativeInfinity, Valid: true}, nil
		}
		if t, err := time.Parse("2006-01-02", key); err == nil {
			return pgtype.Date{Time: t, Valid: true}, nil
		}
	case "title":
		return key, nil
	}
	return nil, apperr.Invalid("cursor", "Invalid cursor")
}

// resolveNames turns names from the query language into IDs within visible spaces.
// Unknown names resolve to a sentinel that matches nothing, so "tag:doesnotexist"
// returns no results rather than being silently ignored.
func (s *Service) resolveNames(ctx context.Context, table string, names []string, spaceIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	lower := make([]string, len(names))
	for i, n := range names {
		lower[i] = strings.ToLower(n)
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM `+table+` WHERE space_id = ANY($1) AND lower(name) = ANY($2)`, spaceIDs, lower)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		ids = []uuid.UUID{uuid.Nil}
	}
	return ids, nil
}

// Search runs a query for the principal. All results are restricted to spaces the
// principal belongs to; this filter is applied before anything else.
func (s *Service) Search(ctx context.Context, p *auth.Principal, q Query) (*Result, error) {
	if p == nil || !p.Has(auth.ScopeRead) {
		return nil, apperr.Unauthorized("")
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return s.run(ctx, visible, q)
}

// Matches reports whether one document satisfies a query, regardless of any user (used
// by workflow conditions). Only the document's own space is considered.
func (s *Service) Matches(ctx context.Context, spaceID, docID uuid.UUID, q Query) (bool, error) {
	q.SpaceIDs, q.IDs, q.Limit, q.Cursor, q.WithTotal, q.Trash, q.Sort, q.Mode = []uuid.UUID{spaceID}, []uuid.UUID{docID}, 1, "", false, false, "-added", ""
	res, err := s.run(ctx, []uuid.UUID{spaceID}, q)
	if err != nil {
		return false, err
	}
	return len(res.Hits) > 0, nil
}

// run executes a query within the given visible spaces.
func (s *Service) run(ctx context.Context, visible []uuid.UUID, q Query) (*Result, error) {
	var err error
	spaceIDs := visible
	if len(q.SpaceIDs) > 0 {
		spaceIDs = nil
		for _, id := range q.SpaceIDs {
			if slices.Contains(visible, id) {
				spaceIDs = append(spaceIDs, id)
			}
		}
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	var known map[string]bool
	if strings.Contains(q.Q, ":") {
		if known, err = s.fieldNames(ctx, visible); err != nil {
			return nil, err
		}
	}
	parsed := ParseWith(q.Q, s.now(), known)
	if len(parsed.Spaces) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT id FROM spaces WHERE id = ANY($1) AND lower(name) = ANY($2)`, spaceIDs, lowerAll(parsed.Spaces))
		if err != nil {
			return nil, err
		}
		if spaceIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return nil, err
		}
	}
	if len(spaceIDs) == 0 {
		return &Result{Hits: []Hit{}}, nil
	}

	b := &builder{}
	spaceArg := b.arg(spaceIDs)
	b.where("d.space_id = ANY(" + spaceArg + ")") // conds[0]: the newest-first query below swaps it for one space at a time
	if len(q.IDs) > 0 {
		b.where("d.id = ANY(" + b.arg(q.IDs) + ")")
	}
	if q.Trash {
		b.where("d.deleted_at IS NOT NULL")
	} else {
		b.where("d.deleted_at IS NULL")
	}

	// Tags
	// The same tag twice would never be counted twice below.
	var tagAll []uuid.UUID
	for _, id := range q.TagIDs {
		if !slices.Contains(tagAll, id) {
			tagAll = append(tagAll, id)
		}
	}
	for _, name := range parsed.Tags { // tag:a tag:b means both, like tag_ids
		ids, err := s.resolveNames(ctx, "tags", []string{name}, spaceIDs)
		if err != nil {
			return nil, err
		}
		// Each named tag may exist in several spaces: require any of the same-named ones.
		b.where("EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id AND dt.tag_id = ANY(" + b.arg(ids) + "))")
	}
	if len(tagAll) > 0 {
		b.where("(SELECT count(*) FROM document_tags dt WHERE dt.document_id=d.id AND dt.tag_id = ANY(" + b.arg(tagAll) + ")) = " + b.arg(len(tagAll)))
	}
	if len(q.AnyTagIDs) > 0 {
		b.where("EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id AND dt.tag_id = ANY(" + b.arg(q.AnyTagIDs) + "))")
	}
	notTags := append([]uuid.UUID{}, q.NotTagIDs...)
	if ids, err := s.resolveNames(ctx, "tags", parsed.NotTags, spaceIDs); err != nil {
		return nil, err
	} else {
		notTags = append(notTags, ids...)
	}
	if len(notTags) > 0 {
		b.where("NOT EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id AND dt.tag_id = ANY(" + b.arg(notTags) + "))")
	}
	if q.Untagged || parsed.Untagged {
		b.where("NOT EXISTS (SELECT 1 FROM document_tags dt WHERE dt.document_id=d.id)")
	}

	// Correspondents & types
	corr := append([]uuid.UUID{}, q.CorrespondentIDs...)
	if ids, err := s.resolveNames(ctx, "correspondents", parsed.Correspondents, spaceIDs); err != nil {
		return nil, err
	} else {
		corr = append(corr, ids...)
	}
	if len(corr) > 0 {
		b.where("d.correspondent_id = ANY(" + b.arg(corr) + ")")
	}
	if q.NoCorrespondent {
		b.where("d.correspondent_id IS NULL")
	}
	types := append([]uuid.UUID{}, q.TypeIDs...)
	if ids, err := s.resolveNames(ctx, "document_types", parsed.Types, spaceIDs); err != nil {
		return nil, err
	} else {
		types = append(types, ids...)
	}
	if len(types) > 0 {
		b.where("d.document_type_id = ANY(" + b.arg(types) + ")")
	}
	if q.NoType {
		b.where("d.document_type_id IS NULL")
	}

	// Custom fields
	for _, f := range append(slices.Clone(q.Custom), parsed.Custom...) {
		b.where(customCond(b, f))
	}

	// Dates
	dateFrom, dateTo := firstNonNil(q.DateFrom, parsed.DateFrom), firstNonNil(q.DateTo, parsed.DateTo)
	if dateFrom != nil {
		b.where("d.document_date >= " + b.arg(*dateFrom) + "::date")
	}
	if dateTo != nil {
		b.where("d.document_date <= " + b.arg(*dateTo) + "::date")
	}
	addedFrom, addedTo := firstNonNil(q.AddedFrom, parsed.AddedFrom), firstNonNil(q.AddedTo, parsed.AddedTo)
	if addedFrom != nil {
		b.where("d.added_at >= " + b.arg(*addedFrom))
	}
	if addedTo != nil {
		b.where("d.added_at < " + b.arg(*addedTo))
	}
	if inbox := firstNonNil(q.Inbox, parsed.Inbox); inbox != nil {
		b.where("d.inbox = " + b.arg(*inbox))
	}
	if q.CollectionID != nil {
		b.where("EXISTS (SELECT 1 FROM collection_items ci WHERE ci.document_id=d.id AND ci.collection_id=" + b.arg(*q.CollectionID) + ")")
	}
	statuses := append(append([]string{}, q.Statuses...), parsed.Statuses...)
	if len(statuses) > 0 {
		b.where("d.status = ANY(" + b.arg(statuses) + ")")
	}
	if len(parsed.Languages) > 0 {
		b.where("d.language = ANY(" + b.arg(parsed.Languages) + ")")
	}
	if parsed.ASN != nil {
		b.where("d.asn = " + b.arg(*parsed.ASN))
	}

	// Full text
	tsq := textindex.TSQuery(parsed.Parts)
	hasText := tsq != ""
	rankExpr := "0::float8"
	if hasText {
		qa := b.arg(tsq)
		raw := strings.TrimSpace(parsed.Raw)
		// Our own lexemes OR English-stemmed lexemes; plus fuzzy title match for typos.
		// The parameter is typed text explicitly: PostgreSQL infers one type per parameter.
		qexpr := "(" + qa + "::text::tsquery || to_tsquery('english', " + qa + "::text))"
		textCond := "(d.meta_fts @@ " + qexpr + " OR d.content_fts @@ " + qexpr
		if len([]rune(raw)) >= 3 {
			textCond += " OR " + b.arg(strings.ToLower(raw)) + " <% lower(d.title)"
		}
		textCond += ")"
		b.where(textCond)
		rankExpr = "(ts_rank(d.meta_fts, " + qexpr + ") * 2 + ts_rank(d.content_fts, " + qexpr + "))::float8"
	}

	// Sorting & pagination
	sort := q.Sort
	if sort == "" {
		if hasText {
			sort = "relevance"
		} else {
			sort = "-added"
		}
	}
	if sort == "relevance" && !hasText {
		sort = "-added"
	}
	// keyExpr is the sort key as it travels in a cursor; ordExpr is what rows are ordered and
	// compared by, written exactly as an index on (space_id, ordExpr, id) has it.
	var keyExpr, ordExpr, dir, cfOrder string
	switch strings.TrimPrefix(sort, "-") {
	case "relevance":
	case "added":
		keyExpr, ordExpr = "to_char(d.added_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.US')", "d.added_at"
	case "date":
		// Undated documents sort as the earliest: last when newest first.
		keyExpr, ordExpr = "coalesce(to_char(d.document_date, 'YYYY-MM-DD'), '-infinity')", "coalesce(d.document_date, '-infinity'::date)"
	case "title":
		keyExpr, ordExpr = "lower(d.title)", "lower(d.title)"
	case "updated":
		keyExpr, ordExpr = "to_char(d.updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.US')", "d.updated_at"
	default:
		base := strings.TrimPrefix(sort, "-")
		if !strings.HasPrefix(base, "cf:") {
			return nil, apperr.Invalid("sort", "Unknown sort order")
		}
		var err error
		if cfOrder, err = s.customOrder(ctx, strings.TrimPrefix(base, "cf:"), spaceIDs); err != nil {
			return nil, err
		}
	}
	dir = "ASC"
	if strings.HasPrefix(sort, "-") {
		dir = "DESC"
	}
	keyset := ordExpr != ""

	where := strings.Join(b.conds, " AND ")
	countArgs := slices.Clone(b.args)

	var cur cursor
	if q.Cursor != "" {
		if cur, err = decodeCursor(q.Cursor); err != nil {
			return nil, err
		}
	}
	var order, pageCond string
	if sort == "relevance" {
		order = "rank DESC, d.id DESC"
	} else if cfOrder != "" {
		// Custom field values have no cheap keyset key, so these sorts page by offset.
		order = cfOrder + " " + dir + " NULLS LAST, d.id " + dir
	} else {
		order = ordExpr + " " + dir + ", d.id " + dir
		if cur.Key != nil {
			key, err := cursorKey(strings.TrimPrefix(sort, "-"), *cur.Key)
			if err != nil {
				return nil, err
			}
			cmp := ">"
			if dir == "DESC" {
				cmp = "<"
			}
			pageCond = " AND (" + ordExpr + ", d.id) " + cmp + " (" + b.arg(key) + ", " + b.arg(cur.ID) + "::uuid)"
		}
	}
	key := "''"
	if keyExpr != "" {
		key = keyExpr
	}
	sqlq := fmt.Sprintf(`SELECT d.id, %s AS rank, %s AS sortkey FROM documents d WHERE %s%s ORDER BY %s LIMIT %d OFFSET %d`,
		rankExpr, key, where, pageCond, order, q.Limit+1, cur.Offset)
	if keyset {
		// Take the first page of each space, which its (space_id, sort key, id) index hands over
		// in order, and merge those. Asked for all spaces at once, PostgreSQL reads and sorts
		// every document the person can see to return sixty of them: 110 ms against 2 ms at
		// 150,000 documents.
		rest := ""
		if len(b.conds) > 1 {
			rest = " AND " + strings.Join(b.conds[1:], " AND ")
		}
		sqlq = fmt.Sprintf(`SELECT x.id, x.rank, x.sortkey FROM unnest(%s::uuid[]) AS sp(id) CROSS JOIN LATERAL (
			SELECT d.id, %s AS rank, %s AS sortkey, %s AS ord FROM documents d WHERE d.space_id = sp.id%s%s ORDER BY %s LIMIT %d) x
			ORDER BY x.ord %s, x.id %s LIMIT %d`,
			spaceArg, rankExpr, key, ordExpr, rest, pageCond, order, q.Limit+1, dir, dir, q.Limit+1)
	}
	// Words in most of the library (see broadMatch): rank the newest RelevanceCap matches, which
	// each space's (space_id, added_at, id) index hands over, instead of every document.
	broad, countCap := false, TotalCap
	if sort == "relevance" && !q.Trash {
		if q.Cursor != "" {
			broad = cur.Broad
		} else {
			broad = s.broadMatch(ctx, where, countArgs, spaceIDs)
		}
	}
	if broad {
		rest := ""
		if len(b.conds) > 1 {
			rest = " AND " + strings.Join(b.conds[1:], " AND ")
		}
		// Each space newest first, the conditions checked on the way until RelevanceCap match. The
		// OFFSET 0 keeps them out of the inner query: the planner underestimates how many documents
		// a word is in and would otherwise collect every match to sort them.
		sqlq = fmt.Sprintf(`SELECT d.id, %s AS rank, '' AS sortkey FROM (
			SELECT x.id FROM unnest(%s::uuid[]) AS sp(id) CROSS JOIN LATERAL (
				SELECT d.id, d.added_at FROM (SELECT * FROM documents d WHERE d.space_id = sp.id AND d.deleted_at IS NULL
					ORDER BY d.added_at DESC, d.id DESC OFFSET 0) d WHERE TRUE%s LIMIT %d) x
			ORDER BY x.added_at DESC, x.id DESC LIMIT %d) c JOIN documents d ON d.id = c.id
			ORDER BY %s LIMIT %d OFFSET %d`,
			rankExpr, spaceArg, rest, RelevanceCap, RelevanceCap, order, q.Limit+1, cur.Offset)
		countCap = RelevanceCap
	}
	// The total and the filter counts are worked out on another connection meanwhile: for a word
	// in most documents, each takes about as long as the page itself.
	type counted struct {
		n      int
		facets *Facets
		err    error
	}
	var counts chan counted
	if (q.WithTotal || q.WithFacets) && q.Cursor == "" {
		counts = make(chan counted, 1)
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			var c counted
			// Counting stops at countCap: an exact count of a huge library costs more than the page itself.
			c.err = s.pool.QueryRow(cctx, `SELECT count(*) FROM (SELECT 1 FROM documents d WHERE `+where+` LIMIT `+strconv.Itoa(countCap+1)+`) x`, countArgs...).Scan(&c.n)
			// Facet counts walk every matching document; past facetCap the filter menus show overall counts.
			if c.err == nil && q.WithFacets && c.n <= facetCap && !broad {
				c.facets, c.err = s.facets(cctx, where, countArgs)
			}
			counts <- c
		}()
	}
	rows, err := s.pool.Query(ctx, sqlq, b.args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	type row struct {
		id   uuid.UUID
		rank float64
		key  string
	}
	var res []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.rank, &r.key); err != nil {
			rows.Close()
			return nil, err
		}
		res = append(res, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := &Result{Hits: make([]Hit, 0, len(res))}
	if len(res) > q.Limit {
		res = res[:q.Limit]
		last := res[len(res)-1]
		if sort == "relevance" || cfOrder != "" {
			out.NextCursor = encodeCursor(cursor{Offset: cur.Offset + q.Limit, Broad: broad})
		} else {
			k := last.key
			out.NextCursor = encodeCursor(cursor{Key: &k, ID: last.id.String()})
		}
	}
	for _, r := range res {
		out.Hits = append(out.Hits, Hit{ID: r.id, Rank: r.rank})
	}

	if hasText && len(out.Hits) > 0 {
		if err := s.addSnippets(ctx, out, tsq, parsed); err != nil {
			return nil, err
		}
	}
	if counts != nil {
		c := <-counts
		if c.err != nil {
			return nil, c.err
		}
		if q.WithTotal {
			total := min(c.n, countCap)
			out.Total = &total
			out.TotalCapped = c.n > countCap
		}
		out.Facets = c.facets
	}
	return out, nil
}

// addSnippets finds the best matching page of each hit and builds a highlighted excerpt.
func (s *Service) addSnippets(ctx context.Context, res *Result, tsq string, parsed Parsed) error {
	ids := make([]uuid.UUID, len(res.Hits))
	for i, h := range res.Hits {
		ids[i] = h.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (document_id) document_id, page_no, text FROM pages
		WHERE document_id = ANY($1) AND fts @@ ($2::text::tsquery || to_tsquery('english', $2::text))
		ORDER BY document_id, ts_rank(fts, ($2::text::tsquery || to_tsquery('english', $2::text))) DESC, page_no`, ids, tsq)
	if err != nil {
		return err
	}
	defer rows.Close()
	var terms []string
	prefixLast := false
	for _, p := range parsed.Parts {
		if !p.Negate {
			terms = append(terms, p.Terms...)
			prefixLast = p.Prefix
		}
	}
	byID := map[uuid.UUID]int{}
	for i, h := range res.Hits {
		byID[h.ID] = i
	}
	for rows.Next() {
		var id uuid.UUID
		var page int
		var text string
		if err := rows.Scan(&id, &page, &text); err != nil {
			return err
		}
		i := byID[id]
		pg := page
		res.Hits[i].MatchedPage = &pg
		res.Hits[i].Snippet = textindex.Snippet(text, terms, prefixLast, 220)
	}
	return rows.Err()
}

func firstNonNil[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

func lowerAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}

// Suggest returns quick title matches for the command palette (search-as-you-type).
type Suggestion struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

func (s *Service) Suggest(ctx context.Context, p *auth.Principal, text string, limit int) ([]Suggestion, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []Suggestion{}, nil
	}
	visible, err := s.spaces.VisibleSpaceIDs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	parts := []textindex.QueryPart{}
	for _, t := range textindex.Terms(text) {
		parts = append(parts, textindex.QueryPart{Terms: []string{t}})
	}
	if len(parts) > 0 {
		parts[len(parts)-1].Prefix = true
	}
	tsq := textindex.TSQuery(parts)
	scan := func(r pgx.CollectableRow) (Suggestion, error) {
		var x Suggestion
		err := r.Scan(&x.ID, &x.Title)
		return x, err
	}
	out := []Suggestion{}
	if tsq != "" {
		// Documents whose title, sender, type or tags have words starting with what was typed,
		// newest first, merged from each space's own index: a few milliseconds even when a word
		// is in tens of thousands of titles, where ranking every one of them took a third of a
		// second.
		rows, err := s.pool.Query(ctx, `SELECT x.id, x.title FROM unnest($1::uuid[]) AS sp(id) CROSS JOIN LATERAL (
			SELECT d.id, d.title, d.added_at FROM documents d
			WHERE d.space_id = sp.id AND d.deleted_at IS NULL AND d.meta_fts @@ $2::text::tsquery
			ORDER BY d.added_at DESC, d.id DESC LIMIT $3) x
			ORDER BY x.added_at DESC, x.id DESC LIMIT $3`, visible, tsq, limit)
		if err != nil {
			return nil, err
		}
		if out, err = pgx.CollectRows(rows, scan); err != nil {
			return nil, err
		}
	}
	if len(out) >= limit || len([]rune(text)) < 3 {
		return out, nil
	}
	// Too few: a typo, perhaps. Titles that look like what was typed, the closest first, among
	// the first few hundred such titles so a common misspelling can't rank a whole library.
	seen := make([]uuid.UUID, len(out))
	for i, x := range out {
		seen[i] = x.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT id, title FROM (
			SELECT d.id, d.title, d.added_at FROM documents d
			WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL AND lower($2::text) <% lower(d.title) AND d.id <> ALL($3)
			LIMIT 500) c
		ORDER BY word_similarity(lower($2::text), lower(title)) DESC, added_at DESC LIMIT $4`, visible, text, seen, limit-len(out))
	if err != nil {
		return nil, err
	}
	more, err := pgx.CollectRows(rows, scan)
	return append(out, more...), err
}

// broadMatch reports whether the conditions match most of the searched documents, and more
// than BroadMin of them. Ranking every one of those by relevance reads the whole library (a
// quarter of a second at 200,000 documents, more with long texts), and among so many the best
// are found in the newest few thousand as well: see RelevanceCap. The planner can't tell (it
// can't size a word search whose query is put together at run time), so the conditions are
// tried on a random sample of about two thousand documents: a few pages read.
func (s *Service) broadMatch(ctx context.Context, where string, args []any, spaceIDs []uuid.UUID) bool {
	totals, err := s.spaces.SpaceTotals(ctx, spaceIDs)
	if err != nil {
		return false
	}
	var n int
	for _, t := range totals {
		n += t.Documents
	}
	if n <= BroadMin {
		return false
	}
	pct := min(100, 2000*100/float64(n))
	// where's first condition is the space filter, on $1.
	var hit, seen int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE `+where+`), count(*) FILTER (WHERE d.space_id = ANY($1) AND d.deleted_at IS NULL)
		FROM documents d TABLESAMPLE SYSTEM (`+strconv.FormatFloat(pct, 'f', 4, 64)+`)`, args...).Scan(&hit, &seen); err != nil || seen == 0 {
		return false
	}
	share := float64(hit) / float64(seen)
	return share > 0.5 && share*float64(n) > float64(BroadMin)
}

// BroadMin and RelevanceCap: a search that matches more than BroadMin documents, and most of the
// library, ranks the newest RelevanceCap matches for "best match" and counts no further. Variables
// so tests can see it work on a small library.
var (
	BroadMin     = 50000
	RelevanceCap = 10000
)

// TotalCap bounds how far result totals are counted; facetCap, above which facets are skipped.
const (
	TotalCap = 100000
	facetCap = 50000
)

// facets counts the matching documents per tag, correspondent, type and status in one round trip.
func (s *Service) facets(ctx context.Context, where string, args []any) (*Facets, error) {
	rows, err := s.pool.Query(ctx, `WITH m AS MATERIALIZED (SELECT d.id, d.correspondent_id, d.document_type_id, d.status FROM documents d WHERE `+where+`)
		SELECT 'tag', dt.tag_id::text, count(*) FROM m JOIN document_tags dt ON dt.document_id = m.id GROUP BY 2
		UNION ALL SELECT 'correspondent', correspondent_id::text, count(*) FROM m WHERE correspondent_id IS NOT NULL GROUP BY 2
		UNION ALL SELECT 'type', document_type_id::text, count(*) FROM m WHERE document_type_id IS NOT NULL GROUP BY 2
		UNION ALL SELECT 'status', status, count(*) FROM m GROUP BY 2`, args...)
	if err != nil {
		return nil, fmt.Errorf("facets: %w", err)
	}
	defer rows.Close()
	f := &Facets{Tags: map[string]int{}, Correspondents: map[string]int{}, Types: map[string]int{}, Statuses: map[string]int{}}
	for rows.Next() {
		var kind, key string
		var n int
		if err := rows.Scan(&kind, &key, &n); err != nil {
			return nil, err
		}
		switch kind {
		case "tag":
			f.Tags[key] = n
		case "correspondent":
			f.Correspondents[key] = n
		case "type":
			f.Types[key] = n
		default:
			f.Statuses[key] = n
		}
	}
	return f, rows.Err()
}
