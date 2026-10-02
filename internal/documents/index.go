package documents

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/textindex"
)

// ReindexMeta rebuilds the metadata search vector of a document: title (A),
// correspondent/type/tags (B), notes/filename/location/custom fields (C). It must be
// called inside the transaction that changed the metadata so search stays consistent.
func ReindexMeta(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var title, filename, location, lang string
	var corr, typ *string
	var tags, notes, fields []string
	err := q.QueryRow(ctx, `SELECT d.title, d.original_filename, d.physical_location, d.language, c.name, t.name,
		coalesce((SELECT array_agg(tg.name) FROM document_tags dt JOIN tags tg ON tg.id=dt.tag_id WHERE dt.document_id=d.id), '{}'),
		coalesce((SELECT array_agg(n.body) FROM notes n WHERE n.document_id=d.id), '{}'),
		coalesce((SELECT array_agg(coalesce(v.value_text, v.value_number::text, to_char(v.value_date,'YYYY-MM-DD'), v.value_json::text))
			FROM custom_field_values v WHERE v.document_id=d.id), '{}')
		FROM documents d LEFT JOIN correspondents c ON c.id=d.correspondent_id LEFT JOIN document_types t ON t.id=d.document_type_id
		WHERE d.id=$1`, id).Scan(&title, &filename, &location, &lang, &corr, &typ, &tags, &notes, &fields)
	if err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	b := textindex.NewBuilder()
	b.Add(title, textindex.A)
	if corr != nil {
		b.Add(*corr, textindex.B)
	}
	if typ != nil {
		b.Add(*typ, textindex.B)
	}
	for _, t := range tags {
		b.Add(t, textindex.B)
	}
	for _, n := range notes {
		b.Add(n, textindex.C)
	}
	for _, f := range fields {
		b.Add(f, textindex.C)
	}
	b.Add(strings.TrimSuffix(filename, extOf(filename)), textindex.C)
	b.Add(location, textindex.C)

	stemSrc := strings.Join(append([]string{title, deref(corr), deref(typ)}, tags...), " ")
	return updateVector(ctx, q, `UPDATE documents SET meta_fts = $2::tsvector || %s WHERE id=$1`, id, b.String(), lang, stemSrc, "A")
}

// ReindexContent rebuilds the content search vector and per-page vectors from the
// pages table. Called after an OCR run becomes current.
func ReindexContent(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var lang string
	if err := q.QueryRow(ctx, `SELECT language FROM documents WHERE id=$1`, id).Scan(&lang); err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	rows, err := q.Query(ctx, `SELECT page_no, text FROM pages WHERE document_id=$1 ORDER BY page_no`, id)
	if err != nil {
		return err
	}
	type page struct {
		no   int
		text string
	}
	var pages []page
	for rows.Next() {
		var p page
		if err := rows.Scan(&p.no, &p.text); err != nil {
			rows.Close()
			return err
		}
		pages = append(pages, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	doc := textindex.NewBuilder()
	var all strings.Builder
	for _, p := range pages {
		pb := textindex.NewBuilder()
		pb.Add(p.text, textindex.D)
		if err := updateVector(ctx, q, `UPDATE pages SET fts = $2::tsvector || %s WHERE document_id=$1 AND page_no=%p`,
			id, pb.String(), lang, p.text, "D", p.no); err != nil {
			return err
		}
		doc.Add(p.text, textindex.D)
		if all.Len() < maxContentBytes {
			all.WriteString(p.text)
			all.WriteString("\n\f\n")
		}
	}
	content := cutUTF8(all.String(), maxContentBytes)
	if _, err := q.Exec(ctx, `UPDATE documents SET content=$2 WHERE id=$1`, id, content); err != nil {
		return err
	}
	return updateVector(ctx, q, `UPDATE documents SET content_fts = $2::tsvector || %s WHERE id=$1`, id, doc.String(), lang, stemSource(content), "D")
}

// maxContentBytes caps stored concatenated content; page-level text is always complete.
const maxContentBytes = 2 << 20

// stemSource limits text passed to to_tsvector (whose output must stay below 1 MB).
func stemSource(s string) string { return cutUTF8(s, 400<<10) }

// cutUTF8 truncates s to at most n bytes without splitting a multi-byte character
// (PostgreSQL rejects invalid UTF-8).
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// updateVector runs an UPDATE where %s becomes the language-specific stemmed vector (an
// empty vector when PostgreSQL has no stemmer for the language) and %p the position of
// the optional extra argument. $1 is the id and $2 our own vector.
func updateVector(ctx context.Context, q db.Querier, tmpl string, id uuid.UUID, vec, lang, stemText, weight string, extra ...any) error {
	args := []any{id, vec}
	stem := "''::tsvector"
	if cfg := textindex.PGConfig(lang); cfg != "" {
		args = append(args, cfg, stemSource(stemText))
		stem = "setweight(to_tsvector($3::regconfig, $4), '" + weight + "')"
	}
	sql := strings.Replace(tmpl, "%s", stem, 1)
	if len(extra) > 0 {
		args = append(args, extra...)
		sql = strings.Replace(sql, "%p", "$"+strconv.Itoa(len(args)), 1)
	}
	_, err := q.Exec(ctx, sql, args...)
	return err
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 6 {
		return name[i:]
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
