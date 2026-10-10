package ai

import "math"

// Prompts are fitted to the provider's context window (Provider.ContextTokens): a model
// given more than it can hold either refuses the request or, worse, quietly drops part of
// it, usually the instructions at the start. Sizes are estimates (see tokenCost), kept on
// the safe side.
const (
	minContextTokens     = 1024
	defaultContextTokens = 8192

	classifyReplyTokens = 600  // room left for the JSON answer
	classifyTextTokens  = 4000 // most document text worth sending, however large the window
	askReplyTokens      = 800  // room left for the answer
	askPassageTokens    = 4000 // most source text given to the model
	historyTokens       = 1500 // most earlier conversation sent along
)

// contextTokens is the provider's context window, with a floor for nonsense values.
func (p *Provider) contextTokens() int {
	if p.ContextTokens <= 0 {
		return defaultContextTokens
	}
	return max(p.ContextTokens, minContextTokens)
}

// estTokens estimates how many model tokens a text uses.
func estTokens(s string) int {
	var n float64
	for _, r := range s {
		n += tokenCost(r)
	}
	return int(math.Ceil(n))
}

// cutToTokens returns the start of s that fits in n tokens.
func cutToTokens(s string, n int) string {
	var used float64
	for i, r := range s {
		if used += tokenCost(r); used > float64(n) {
			return s[:i]
		}
	}
	return s
}

// tailTokens returns the end of s that fits in n tokens.
func tailTokens(s string, n int) string {
	r := []rune(s)
	var used float64
	for i := len(r) - 1; i >= 0; i-- {
		if used += tokenCost(r[i]); used > float64(n) {
			return string(r[i+1:])
		}
	}
	return s
}

// excerpt is the part of a document shown to the model when it doesn't fit in n tokens:
// mostly its start (letterhead, title, date, who it is from), and a little of its end
// (totals, signatures).
func excerpt(content string, n int) string {
	if estTokens(content) <= n {
		return content
	}
	tail := n / 7
	return cutToTokens(content, n-tail) + "\n[…]\n" + tailTokens(content, tail)
}
