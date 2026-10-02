package pipeline

import (
	"testing"
	"time"
)

func TestExtractDate(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		text     string
		dayFirst bool
		want     string
	}{
		{"Bill Date: 05/08/2026 Due 20/08/2026", true, "2026-08-05"},
		{"Bill Date: 08/05/2026", false, "2026-08-05"},
		{"Invoice 2026-03-15", true, "2026-03-15"},
		{"Dated 5th August 2026", true, "2026-08-05"},
		{"August 5, 2026", true, "2026-08-05"},
		{"Issued 05-Aug-26", true, "2026-08-05"},
		{"Date 13/02/2026", false, "2026-02-13"}, // impossible MM/DD falls back to DD/MM
		{"No date here 12345", true, ""},
		{"Ref 99/99/2026 then 01.02.2025", true, "2025-02-01"},
		{"Future 01/01/2099", true, ""},
	}
	for _, c := range cases {
		got, ok := ExtractDate(c.text, c.dayFirst, now)
		s := ""
		if ok {
			s = got.Format("2006-01-02")
		}
		if s != c.want {
			t.Errorf("%q: got %q want %q", c.text, s, c.want)
		}
	}
}

func TestHasUsableText(t *testing.T) {
	if HasUsableText("   \n ") || HasUsableText("abc") {
		t.Fatal("short text should not be usable")
	}
	if !HasUsableText("This is a perfectly normal electricity bill text") {
		t.Fatal("normal text should be usable")
	}
	if !HasUsableText("यह एक सामान्य बिजली बिल का पाठ है जो पढ़ने योग्य है") {
		t.Fatal("Hindi text should be usable")
	}
	if HasUsableText("���������������������") {
		t.Fatal("garbage should not be usable")
	}
}
