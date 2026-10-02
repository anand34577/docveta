package textindex

import (
	"strings"
	"testing"
)

func words(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

func TestTokenizeKeepsIndicWordsWhole(t *testing.T) {
	// "बिजली बिल" (electricity bill) contains vowel signs that must not split words.
	got := words(Tokenize("बिजली बिल, दिनांक 05/08/2026"))
	want := []string{"बिजली", "बिल", "दिनांक", "05", "08", "2026"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", got, want)
	}
	// Tamil
	if got := words(Tokenize("மின்சாரம் கட்டணம்")); len(got) != 2 {
		t.Fatalf("tamil split wrong: %v", got)
	}
}

func TestTokenizeLowercasesAndNormalizes(t *testing.T) {
	got := words(Tokenize("Électricité BILL"))
	if got[0] != "électricité" || got[1] != "bill" {
		t.Fatalf("got %v", got)
	}
	// Decomposed é (e + combining acute) normalizes to the composed form.
	if words(Tokenize("Électricité"))[0] != "électricité" {
		t.Fatal("NFC normalization failed")
	}
}

func TestBuilderAddsIdentifierVariants(t *testing.T) {
	b := NewBuilder()
	b.Add("PAN: ABCDE-1234-F policy 12/345/678", D)
	s := b.String()
	for _, want := range []string{"'abcde1234f'", "'12345678'", "'abcde'", "'policy'"} {
		if !strings.Contains(s, want) {
			t.Errorf("vector %q missing %s", s, want)
		}
	}
}

func TestBuilderWeightsAndQuoting(t *testing.T) {
	b := NewBuilder()
	b.Add("O'Brien bill", A)
	s := b.String()
	if !strings.Contains(s, "'o''brien'") && !strings.Contains(s, "'o'") {
		t.Fatalf("quoting: %s", s)
	}
	if !strings.Contains(s, "'bill':") || !strings.Contains(s, "A") {
		t.Fatalf("weights: %s", s)
	}
}

func TestTSQuery(t *testing.T) {
	q := TSQuery([]QueryPart{
		{Terms: []string{"electric"}, Prefix: true},
		{Terms: []string{"due", "date"}},
		{Terms: []string{"draft"}, Negate: true},
	})
	want := "'electric':* & ('due' <-> 'date') & !'draft'"
	if q != want {
		t.Fatalf("got %q want %q", q, want)
	}
	if TSQuery([]QueryPart{{Terms: []string{"x"}, Negate: true}}) != "" {
		t.Fatal("purely negative query should be empty")
	}
	if got := TSQuery([]QueryPart{{Terms: []string{`it's\`}}}); got != `'it''s\\'` {
		t.Fatalf("escaping: %q", got)
	}
}

func TestSnippetMarksHits(t *testing.T) {
	text := strings.Repeat("filler ", 50) + "Your electricity bill is due on 20 August" + strings.Repeat(" more", 50)
	segs := Snippet(text, []string{"electricity", "due"}, false, 80)
	var hits []string
	for _, s := range segs {
		if s.Hit {
			hits = append(hits, s.Text)
		}
	}
	if strings.Join(hits, ",") != "electricity,due" {
		t.Fatalf("hits %v in %+v", hits, segs)
	}
	if !strings.HasPrefix(segs[0].Text, "…") || !strings.HasSuffix(segs[len(segs)-1].Text, "…") {
		t.Fatalf("missing ellipses: %+v", segs)
	}
}

func TestSnippetPrefix(t *testing.T) {
	segs := Snippet("BESCOM electricity", []string{"elec"}, true, 100)
	found := false
	for _, s := range segs {
		if s.Hit && s.Text == "electricity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("prefix hit missing: %+v", segs)
	}
}
