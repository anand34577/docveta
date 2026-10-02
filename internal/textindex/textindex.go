// Package textindex builds PostgreSQL tsvector/tsquery literals from text using a
// Unicode-aware tokenizer written in Go.
//
// Why not to_tsvector('simple', ...)? PostgreSQL's default parser decides word
// characters using the C library locale; with several locales it splits Indic words
// at combining vowel signs (matras), which breaks Hindi/Marathi/Tamil search. Doing
// tokenization here makes behaviour identical on every platform and lets us add
// identifier variants (PAN, policy numbers) as extra lexemes. Stemmed lexemes for
// languages PostgreSQL supports are still added with to_tsvector(<lang>, ...).
package textindex

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxLexemeBytes     = 200   // longer tokens are noise (base64 blobs, etc.)
	maxPositionsPerLex = 8     // enough for ranking; keeps vectors small
	maxPosition        = 16383 // PostgreSQL limit
	maxLexemes         = 60000 // keeps the tsvector well under the 1 MB limit
)

// Token is a normalized word with its position (1-based) in the text.
type Token struct {
	Text string
	Pos  int
}

// Normalize applies NFC and removes zero-width characters that only affect rendering,
// plus NUL bytes (which PostgreSQL text columns reject) and invalid UTF-8.
func Normalize(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = norm.NFC.String(s)
	return strings.Map(func(r rune) rune {
		switch r {
		case 0, 0x200B, 0xFEFF, 0x00AD:
			return -1
		}
		return r
	}, s)
}

// IsWordRune reports whether r is part of a word for indexing purposes.
func IsWordRune(r rune) bool { return isWordRune(r) }

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) ||
		r == 0x200C || r == 0x200D // ZWNJ/ZWJ are meaningful inside Indic words
}

// Tokenize splits text into lower-cased word tokens. Joiners inside words are kept for
// fidelity but stripped from the lexeme so searches match with or without them.
func Tokenize(text string) []Token {
	text = Normalize(text)
	var out []Token
	pos := 0
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		w := strings.Map(func(r rune) rune {
			if r == 0x200C || r == 0x200D {
				return -1
			}
			return unicode.ToLower(r)
		}, text[start:end])
		start = -1
		if w == "" || len(w) > maxLexemeBytes {
			return
		}
		pos++
		out = append(out, Token{Text: w, Pos: pos})
	}
	for i, r := range text {
		if isWordRune(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(text))
	return out
}

// identifierVariants returns joined forms of identifier-like sequences so that
// "ABCDE-1234-F", "ABCDE 1234 F" and "abcde1234f" match each other.
func identifierVariants(text string) []string {
	var out []string
	for _, field := range strings.Fields(Normalize(text)) {
		if !strings.ContainsAny(field, "-/._") {
			continue
		}
		hasDigit := strings.IndexFunc(field, unicode.IsDigit) >= 0
		if !hasDigit {
			continue
		}
		joined := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, field)
		if utf8.RuneCountInString(joined) >= 4 && len(joined) <= maxLexemeBytes {
			out = append(out, joined)
		}
	}
	return out
}

// Weight is a tsvector weight label.
type Weight byte

const (
	A Weight = 'A'
	B Weight = 'B'
	C Weight = 'C'
	D Weight = 'D'
)

// Builder accumulates lexemes with positions and weights and renders a tsvector literal.
type Builder struct {
	lex  map[string][]string // lexeme -> "pos+weight" entries
	next int                 // position offset for the next Add
}

func NewBuilder() *Builder { return &Builder{lex: map[string][]string{}} }

// Add tokenizes text and adds its lexemes with weight w.
func (b *Builder) Add(text string, w Weight) {
	if strings.TrimSpace(text) == "" {
		return
	}
	toks := Tokenize(text)
	for _, t := range toks {
		b.addLexeme(t.Text, b.next+t.Pos, w)
	}
	last := b.next
	if len(toks) > 0 {
		last = b.next + toks[len(toks)-1].Pos
	}
	for _, v := range identifierVariants(text) {
		b.addLexeme(v, last, w)
	}
	// Leave a gap so phrases don't match across fields.
	b.next = last + 10
}

func (b *Builder) addLexeme(lex string, pos int, w Weight) {
	ps, exists := b.lex[lex]
	if !exists && len(b.lex) >= maxLexemes {
		return
	}
	if len(ps) >= maxPositionsPerLex {
		return
	}
	if pos > maxPosition {
		pos = maxPosition
	}
	entry := strconv.Itoa(pos)
	if w != D {
		entry += string(w)
	}
	b.lex[lex] = append(ps, entry)
}

// Len returns the number of distinct lexemes.
func (b *Builder) Len() int { return len(b.lex) }

// String renders a tsvector literal, e.g. 'bill':1A,5 'bescom':2A
func (b *Builder) String() string {
	keys := make([]string, 0, len(b.lex))
	for k := range b.lex {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(quote(k))
		sb.WriteByte(':')
		sb.WriteString(strings.Join(b.lex[k], ","))
	}
	return sb.String()
}

// quote renders a lexeme as a tsvector/tsquery quoted literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `''`)
	return "'" + s + "'"
}

// PGConfig maps a language code to a PostgreSQL text search configuration used for
// stemming, or "" when PostgreSQL has no stemmer for that language.
func PGConfig(lang string) string {
	switch strings.ToLower(strings.SplitN(lang, "-", 2)[0]) {
	case "en":
		return "english"
	case "de":
		return "german"
	case "fr":
		return "french"
	case "es":
		return "spanish"
	case "it":
		return "italian"
	case "pt":
		return "portuguese"
	case "nl":
		return "dutch"
	case "sv":
		return "swedish"
	case "da":
		return "danish"
	case "no", "nb", "nn":
		return "norwegian"
	case "fi":
		return "finnish"
	case "ru":
		return "russian"
	case "tr":
		return "turkish"
	case "hu":
		return "hungarian"
	case "ro":
		return "romanian"
	case "id":
		return "indonesian"
	case "ta":
		return "tamil"
	case "ne":
		return "nepali"
	}
	return ""
}
