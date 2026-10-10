package textindex

import (
	"strings"
	"unicode/utf8"
)

// QueryPart is one element of a parsed free-text query.
type QueryPart struct {
	Terms  []string // a phrase has several terms
	Negate bool
	Prefix bool // the (last) term is matched as a prefix (search-as-you-type)
}

// TSQuery renders parts into a tsquery literal over our own lexemes:
//
//	'electric':* & 'bill' & !'draft' & ('due' <-> 'date')
//
// Returns "" when no usable terms exist.
func TSQuery(parts []QueryPart) string {
	var clauses []string
	positive := 0
	for _, p := range parts {
		if len(p.Terms) == 0 {
			continue
		}
		var c string
		if len(p.Terms) == 1 {
			c = quote(p.Terms[0])
			if p.Prefix {
				c += ":*"
			}
		} else {
			qs := make([]string, len(p.Terms))
			for i, t := range p.Terms {
				qs[i] = quote(t)
			}
			if p.Prefix { // still being typed: its last piece may be unfinished
				qs[len(qs)-1] += ":*"
			}
			c = "(" + strings.Join(qs, " <-> ") + ")"
		}
		if p.Negate {
			c = "!" + c
		} else {
			positive++
		}
		clauses = append(clauses, c)
	}
	if positive == 0 {
		return "" // a purely negative query would match everything; treat as no text filter
	}
	return strings.Join(clauses, " & ")
}

// Terms tokenizes free text into query terms.
func Terms(text string) []string {
	toks := Tokenize(text)
	out := make([]string, 0, len(toks)+1)
	for _, t := range toks {
		out = append(out, t.Text)
	}
	out = append(out, identifierVariants(text)...)
	return out
}

// Segment is a piece of a snippet; Hit marks matched words.
type Segment struct {
	Text string `json:"text"`
	Hit  bool   `json:"hit,omitempty"`
}

// Snippet returns a short excerpt of text around the first matching term, with
// matches marked. Matching uses the same normalization as indexing, so it works for
// every script. When prefixLast is set, the last term also matches word prefixes.
func Snippet(text string, terms []string, prefixLast bool, maxRunes int) []Segment {
	text = strings.Join(strings.Fields(Normalize(text)), " ")
	if text == "" {
		return nil
	}
	isHit := func(word string) bool {
		w := strings.ToLower(word)
		for i, t := range terms {
			if w == t || (prefixLast && i == len(terms)-1 && strings.HasPrefix(w, t)) {
				return true
			}
		}
		return false
	}
	type span struct{ start, end int }
	var hits []span
	start := -1
	for i, r := range text + " " {
		if i < len(text) && isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if isHit(text[start:i]) {
				hits = append(hits, span{start, i})
			}
			start = -1
		}
	}

	// Window: start ~1/3 of maxRunes before the first hit.
	from := 0
	if len(hits) > 0 {
		from = hits[0].start
		for back := maxRunes / 3; back > 0 && from > 0; back-- {
			_, sz := utf8.DecodeLastRuneInString(text[:from])
			from -= sz
		}
	}
	to := from
	for n := 0; n < maxRunes && to < len(text); n++ {
		_, sz := utf8.DecodeRuneInString(text[to:])
		to += sz
	}

	var segs []Segment
	cur := from
	for _, h := range hits {
		if h.end <= from || h.start >= to {
			continue
		}
		s, e := max(h.start, from), min(h.end, to)
		if s > cur {
			segs = append(segs, Segment{Text: text[cur:s]})
		}
		segs = append(segs, Segment{Text: text[s:e], Hit: true})
		cur = e
	}
	if cur < to {
		segs = append(segs, Segment{Text: text[cur:to]})
	}
	if from > 0 {
		segs[0].Text = "…" + segs[0].Text
	}
	if to < len(text) {
		segs[len(segs)-1].Text += "…"
	}
	return segs
}
