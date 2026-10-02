package pipeline

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var months = map[string]time.Month{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3, "apr": 4, "april": 4,
	"may": 5, "jun": 6, "june": 6, "jul": 7, "july": 7, "aug": 8, "august": 8, "sep": 9, "sept": 9,
	"september": 9, "oct": 10, "october": 10, "nov": 11, "november": 11, "dec": 12, "december": 12,
}

var (
	reNumeric = regexp.MustCompile(`\b(\d{1,4})[./-](\d{1,2})[./-](\d{2,4})\b`)
	reDayMon  = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?[\s.-]*(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sept?(?:ember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)[\s.,-]*(\d{4}|\d{2})\b`)
	reMonDay  = regexp.MustCompile(`(?i)\b(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sept?(?:ember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)[\s.]+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})\b`)
)

func fullYear(y int) int {
	if y < 100 {
		if y > 50 {
			return 1900 + y
		}
		return 2000 + y
	}
	return y
}

func mk(y, m, d int, now time.Time) (time.Time, bool) {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d { // e.g. 31/02
		return time.Time{}, false
	}
	if y < 1900 || t.After(now.AddDate(1, 0, 0)) {
		return time.Time{}, false
	}
	return t, true
}

// ExtractDate finds the first plausible date in text. dayFirst selects DD/MM vs MM/DD
// interpretation for ambiguous numeric dates. Only the first ~4000 characters are
// examined: the document date is almost always near the top.
func ExtractDate(text string, dayFirst bool, now time.Time) (time.Time, bool) {
	if len(text) > 4000 {
		text = text[:4000]
	}
	type cand struct {
		pos int
		t   time.Time
	}
	var best *cand
	consider := func(pos int, t time.Time, ok bool) {
		if ok && (best == nil || pos < best.pos) {
			best = &cand{pos, t}
		}
	}
	for _, m := range reNumeric.FindAllStringSubmatchIndex(text, 20) {
		a, _ := strconv.Atoi(text[m[2]:m[3]])
		b, _ := strconv.Atoi(text[m[4]:m[5]])
		c, _ := strconv.Atoi(text[m[6]:m[7]])
		switch {
		case m[3]-m[2] == 4: // YYYY-MM-DD
			t, ok := mk(a, b, c, now)
			consider(m[0], t, ok)
		case m[7]-m[6] == 4 || m[7]-m[6] == 2:
			y := fullYear(c)
			if dayFirst {
				t, ok := mk(y, b, a, now)
				if !ok {
					t, ok = mk(y, a, b, now)
				}
				consider(m[0], t, ok)
			} else {
				t, ok := mk(y, a, b, now)
				if !ok {
					t, ok = mk(y, b, a, now)
				}
				consider(m[0], t, ok)
			}
		}
	}
	for _, m := range reDayMon.FindAllStringSubmatchIndex(text, 20) {
		d, _ := strconv.Atoi(text[m[2]:m[3]])
		mon := months[strings.ToLower(text[m[4]:m[5]])]
		y, _ := strconv.Atoi(text[m[6]:m[7]])
		t, ok := mk(fullYear(y), int(mon), d, now)
		consider(m[0], t, ok)
	}
	for _, m := range reMonDay.FindAllStringSubmatchIndex(text, 20) {
		mon := months[strings.ToLower(text[m[2]:m[3]])]
		d, _ := strconv.Atoi(text[m[4]:m[5]])
		y, _ := strconv.Atoi(text[m[6]:m[7]])
		t, ok := mk(y, int(mon), d, now)
		consider(m[0], t, ok)
	}
	if best == nil {
		return time.Time{}, false
	}
	return best.t, true
}
