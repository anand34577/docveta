package search

import (
	"testing"
	"time"

	"github.com/anand34577/docveta/internal/textindex"
)

func TestParseFiltersAndText(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	p := Parse(`electricity "due date" -draft tag:utilities -tag:old from:"HDFC Bank" type:bill date:2026-01..2026-06 is:inbox lang:hi asn:42 untagged`, now)

	if len(p.Tags) != 1 || p.Tags[0] != "utilities" {
		t.Errorf("tags %v", p.Tags)
	}
	if len(p.NotTags) != 1 || p.NotTags[0] != "old" {
		t.Errorf("not tags %v", p.NotTags)
	}
	if len(p.Correspondents) != 1 || p.Correspondents[0] != "HDFC Bank" {
		t.Errorf("correspondents %v", p.Correspondents)
	}
	if *p.DateFrom != "2026-01-01" || *p.DateTo != "2026-06-30" {
		t.Errorf("date range %v..%v", *p.DateFrom, *p.DateTo)
	}
	if p.Inbox == nil || !*p.Inbox || p.ASN == nil || *p.ASN != 42 || !p.Untagged || p.Languages[0] != "hi" {
		t.Errorf("flags: inbox=%v asn=%v untagged=%v langs=%v", p.Inbox, p.ASN, p.Untagged, p.Languages)
	}
	q := textindex.TSQuery(p.Parts)
	want := "'electricity' & ('due' <-> 'date') & !'draft'"
	if q != want {
		t.Errorf("tsquery %q want %q", q, want)
	}
	if p.Raw != `electricity due date` {
		t.Errorf("raw %q", p.Raw)
	}
}

func TestParseLastWordIsPrefix(t *testing.T) {
	p := Parse("electr", time.Now())
	if q := textindex.TSQuery(p.Parts); q != "'electr':*" {
		t.Fatalf("got %q", q)
	}
}

func TestParseIdentifierJoined(t *testing.T) {
	p := Parse("ABCDE-1234-F", time.Now())
	if len(p.Parts) != 1 || p.Parts[0].Terms[0] != "abcde1234f" {
		t.Fatalf("got %+v", p.Parts)
	}
}

func TestParseUnknownKeyIsText(t *testing.T) {
	p := Parse("http://example.com note:abc", time.Now())
	if len(p.Parts) == 0 {
		t.Fatal("unknown keys must be searched as text")
	}
}

func TestQuotedKeyIsText(t *testing.T) {
	p := Parse(`"tag:x"`, time.Now())
	if len(p.Tags) != 0 || len(p.Parts) != 1 {
		t.Fatalf("quoted key should be text: %+v", p)
	}
}

func TestDateRangeForms(t *testing.T) {
	cases := map[string][2]string{
		"2025":                   {"2025-01-01", "2025-12-31"},
		"2024-02":                {"2024-02-01", "2024-02-29"},
		"2026-03-15":             {"2026-03-15", "2026-03-15"},
		"2025-04-01..2026-03-31": {"2025-04-01", "2026-03-31"},
	}
	for in, want := range cases {
		f, to := dateRange(in)
		if f == nil || to == nil || *f != want[0] || *to != want[1] {
			t.Errorf("%s: got %v %v", in, f, to)
		}
	}
	if f, to := dateRange(">2025"); f == nil || *f != "2025-01-01" || to != nil {
		t.Errorf(">2025: %v %v", f, to)
	}
}

func TestRelativeAdded(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	from, to := relRange("30d", now)
	if from == nil || to != nil || !from.Equal(now.AddDate(0, 0, -30)) {
		t.Errorf("30d: %v %v", from, to)
	}
	from, to = relRange(">1y", now)
	if from != nil || to == nil || !to.Equal(now.AddDate(-1, 0, 0)) {
		t.Errorf(">1y: %v %v", from, to)
	}
}
