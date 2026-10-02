package taxonomy

import (
	"regexp"
	"strings"
	"sync"

	"github.com/anand34577/docveta/internal/textindex"
)

// Rule is the matching configuration of a tag/correspondent/type.
type Rule struct {
	Algorithm     string
	Pattern       string
	CaseSensitive bool
}

// MatchText is prepared document text for repeated rule evaluation.
type MatchText struct {
	raw        string
	lower      string
	words      map[string]bool // lower-cased tokens
	wordsCS    map[string]bool // case-preserved tokens
	lowerWords []string
}

func NewMatchText(text string) *MatchText {
	text = textindex.Normalize(text)
	m := &MatchText{raw: text, lower: strings.ToLower(text), words: map[string]bool{}, wordsCS: map[string]bool{}}
	for _, t := range textindex.Tokenize(text) {
		m.words[t.Text] = true
		m.lowerWords = append(m.lowerWords, t.Text)
	}
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !textindex.IsWordRune(r) }) {
		m.wordsCS[w] = true
	}
	return m
}

// splitPattern splits "foo \"bar baz\" qux" into ["foo", "bar baz", "qux"].
func splitPattern(p string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, r := range p {
		switch {
		case r == '"':
			flush()
			inQuote = !inQuote
		case (r == ' ' || r == ',') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func (m *MatchText) hasTerm(term string, cs bool) bool {
	if strings.ContainsRune(term, ' ') {
		if cs {
			return strings.Contains(m.raw, term)
		}
		return strings.Contains(m.lower, strings.ToLower(term))
	}
	if cs {
		return m.wordsCS[term]
	}
	toks := textindex.Tokenize(term)
	if len(toks) == 1 {
		return m.words[toks[0].Text]
	}
	return strings.Contains(m.lower, strings.ToLower(term))
}

var (
	reCacheMu sync.Mutex
	reCache   = map[string]*regexp.Regexp{}
)

func compile(pattern string, cs bool) *regexp.Regexp {
	key := pattern
	if !cs {
		key = "(?i)" + pattern
	}
	reCacheMu.Lock()
	defer reCacheMu.Unlock()
	if re, ok := reCache[key]; ok {
		return re
	}
	re, err := regexp.Compile(key)
	if err != nil {
		re = nil
	}
	if len(reCache) > 1000 {
		reCache = map[string]*regexp.Regexp{}
	}
	reCache[key] = re
	return re
}

// Matches evaluates a rule against prepared text.
func (r Rule) Matches(m *MatchText) bool {
	p := strings.TrimSpace(r.Pattern)
	if p == "" || r.Algorithm == "none" || r.Algorithm == "" {
		return false
	}
	switch r.Algorithm {
	case "any":
		for _, t := range splitPattern(p) {
			if m.hasTerm(t, r.CaseSensitive) {
				return true
			}
		}
	case "all":
		terms := splitPattern(p)
		for _, t := range terms {
			if !m.hasTerm(t, r.CaseSensitive) {
				return false
			}
		}
		return len(terms) > 0
	case "exact":
		if r.CaseSensitive {
			return strings.Contains(m.raw, p)
		}
		return strings.Contains(m.lower, strings.ToLower(p))
	case "regex":
		if re := compile(p, r.CaseSensitive); re != nil {
			return re.MatchString(m.raw)
		}
	case "fuzzy":
		return fuzzyContains(m.lowerWords, p)
	}
	return false
}

// fuzzyContains reports whether some window of words in text is within ~15% edit
// distance of the phrase — tolerant of OCR errors like "BESC0M" vs "BESCOM".
func fuzzyContains(words []string, phrase string) bool {
	pw := textindex.Tokenize(phrase)
	if len(pw) == 0 {
		return false
	}
	target := make([]string, len(pw))
	for i, t := range pw {
		target[i] = t.Text
	}
	tj := []rune(strings.Join(target, " "))
	maxDist := max(1, len(tj)*15/100)
	n := len(target)
	const maxWords = 20000 // bound the work on huge documents
	for i := 0; i+n <= len(words) && i < maxWords; i++ {
		cand := []rune(strings.Join(words[i:i+n], " "))
		if abs(len(cand)-len(tj)) > maxDist {
			continue
		}
		if levenshtein(cand, tj, maxDist) <= maxDist {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// levenshtein computes edit distance with early exit once it exceeds limit.
func levenshtein(a, b []rune, limit int) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > limit {
			return limit + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
