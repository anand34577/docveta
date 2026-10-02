// Package search implements document search: a small query language, PostgreSQL
// full-text + trigram matching, filters, sorting and keyset pagination (DESIGN §12).
package search

import (
	"strings"
	"time"
	"unicode"

	"github.com/anand34577/docveta/internal/textindex"
)

// Parsed is the result of parsing the user's query string. Name-based filters (tag:,
// from:, type:, space:) are resolved to IDs by the service.
type Parsed struct {
	Parts          []textindex.QueryPart
	Raw            string // free text only, for trigram matching
	Tags           []string
	NotTags        []string
	Correspondents []string
	Types          []string
	Spaces         []string
	DateFrom       *string
	DateTo         *string
	AddedFrom      *time.Time
	AddedTo        *time.Time
	Inbox          *bool
	Statuses       []string
	Languages      []string
	ASN            *int64
	Untagged       bool
}

type token struct {
	text       string
	quoted     bool // contains a quoted segment
	startQuote bool // begins with a quote: never a key:value filter
	negate     bool
}

func lex(q string) []token {
	var out []token
	rs := []rune(q)
	for i := 0; i < len(rs); {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		t := token{}
		if rs[i] == '-' && i+1 < len(rs) && !unicode.IsSpace(rs[i+1]) {
			t.negate = true
			i++
		}
		t.startQuote = rs[i] == '"'
		var sb strings.Builder
		for i < len(rs) && !unicode.IsSpace(rs[i]) {
			if rs[i] == '"' {
				// quoted segment (may follow key:)
				i++
				for i < len(rs) && rs[i] != '"' {
					sb.WriteRune(rs[i])
					i++
				}
				i++ // closing quote (or end)
				t.quoted = true
				continue
			}
			sb.WriteRune(rs[i])
			i++
		}
		t.text = sb.String()
		if t.text != "" {
			out = append(out, t)
		}
	}
	return out
}

// Parse parses the query language:
//
//	electricity "due date" -draft tag:utilities -tag:old from:bescom type:bill
//	space:family date:2026 date:2026-01..2026-06 added:>30d is:inbox is:processing
//	lang:hi asn:123 untagged
func Parse(q string, now time.Time) Parsed {
	var p Parsed
	var free []string
	toks := lex(q)
	for i, t := range toks {
		key, val, hasKey := strings.Cut(t.text, ":")
		if hasKey && !t.startQuote && val != "" {
			k := strings.ToLower(key)
			switch k {
			case "tag", "tags":
				if t.negate {
					p.NotTags = append(p.NotTags, val)
				} else {
					p.Tags = append(p.Tags, val)
				}
				continue
			case "from", "correspondent", "corr":
				p.Correspondents = append(p.Correspondents, val)
				continue
			case "type":
				p.Types = append(p.Types, val)
				continue
			case "space":
				p.Spaces = append(p.Spaces, val)
				continue
			case "date", "created":
				p.DateFrom, p.DateTo = dateRange(val)
				continue
			case "added":
				p.AddedFrom, p.AddedTo = relRange(val, now)
				continue
			case "is":
				switch strings.ToLower(val) {
				case "inbox":
					b := !t.negate
					p.Inbox = &b
				case "processing", "failed", "ready":
					p.Statuses = append(p.Statuses, strings.ToLower(val))
				case "locked", "encrypted":
					p.Statuses = append(p.Statuses, "needs_password")
				}
				continue
			case "lang", "language":
				p.Languages = append(p.Languages, strings.ToLower(val))
				continue
			case "asn":
				if n, ok := parseInt(val); ok {
					p.ASN = &n
				}
				continue
			}
		}
		if strings.EqualFold(t.text, "untagged") && !t.quoted {
			p.Untagged = true
			continue
		}
		terms := textindex.Terms(t.text)
		if len(terms) == 0 {
			continue
		}
		part := textindex.QueryPart{Terms: terms, Negate: t.negate}
		if !t.quoted && len(terms) > 1 {
			// "ABCDE-1234" produced several tokens plus a joined variant: match the joined one.
			part.Terms = terms[len(terms)-1:]
		}
		// Prefix-match the last bare word for search-as-you-type.
		if i == len(toks)-1 && !t.quoted && !t.negate && len(part.Terms) == 1 {
			part.Prefix = true
		}
		p.Parts = append(p.Parts, part)
		if !t.negate {
			free = append(free, t.text)
		}
	}
	p.Raw = strings.Join(free, " ")
	return p
}

func parseInt(s string) (int64, bool) {
	var n int64
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int64(r-'0')
	}
	return n, true
}

// dateRange parses 2026, 2026-03, 2026-03-15, a..b, >2026-01-01, <2026.
func dateRange(v string) (from, to *string) {
	expand := func(s string, end bool) *string {
		layouts := []struct {
			l    string
			step func(time.Time) time.Time
		}{
			{"2006-01-02", func(t time.Time) time.Time { return t }},
			{"2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, -1) }},
			{"2006", func(t time.Time) time.Time { return t.AddDate(1, 0, -1) }},
		}
		for _, l := range layouts {
			if t, err := time.Parse(l.l, s); err == nil {
				if end {
					t = l.step(t)
				}
				r := t.Format("2006-01-02")
				return &r
			}
		}
		return nil
	}
	switch {
	case strings.Contains(v, ".."):
		a, b, _ := strings.Cut(v, "..")
		return expand(a, false), expand(b, true)
	case strings.HasPrefix(v, ">="), strings.HasPrefix(v, ">"):
		return expand(strings.TrimLeft(v, ">="), false), nil
	case strings.HasPrefix(v, "<="), strings.HasPrefix(v, "<"):
		return nil, expand(strings.TrimLeft(v, "<="), true)
	}
	return expand(v, false), expand(v, true)
}

// relRange parses added:>30d, added:<1y, added:7d (within the last 7 days) and absolute dates.
func relRange(v string, now time.Time) (from, to *time.Time) {
	op := ""
	if strings.HasPrefix(v, ">") || strings.HasPrefix(v, "<") {
		op, v = v[:1], v[1:]
	}
	if len(v) >= 2 {
		if n, ok := parseInt(v[:len(v)-1]); ok {
			var t time.Time
			switch v[len(v)-1] {
			case 'd':
				t = now.AddDate(0, 0, -int(n))
			case 'w':
				t = now.AddDate(0, 0, -7*int(n))
			case 'm':
				t = now.AddDate(0, -int(n), 0)
			case 'y':
				t = now.AddDate(-int(n), 0, 0)
			default:
				goto absolute
			}
			if op == ">" { // older than
				return nil, &t
			}
			return &t, nil
		}
	}
absolute:
	f, tt := dateRange(v)
	if f != nil {
		if t, err := time.Parse("2006-01-02", *f); err == nil && op != "<" {
			from = &t
		}
	}
	if tt != nil {
		if t, err := time.Parse("2006-01-02", *tt); err == nil && op != ">" {
			t = t.AddDate(0, 0, 1)
			to = &t
		}
	}
	return from, to
}
