package pipeline

import "testing"

func TestMainLanguage(t *testing.T) {
	cases := []struct {
		pages []OCRPage
		want  string
	}{
		{nil, ""},
		{[]OCRPage{{Text: "abc"}}, ""}, // engines that don't report a language
		{[]OCRPage{{Text: "नमस्ते दुनिया", Language: "hi"}}, "hi"},
		{[]OCRPage{{Text: "short", Language: "en"}, {Text: "बहुत लंबा हिंदी पाठ यहाँ है", Language: "hi-IN"}}, "hi"},
		{[]OCRPage{{Text: "a long English page of text", Language: "en"}, {Text: "छोटा", Language: "hi"}}, "en"},
	}
	for i, c := range cases {
		if got := mainLanguage(c.pages); got != c.want {
			t.Errorf("case %d: got %q, want %q", i, got, c.want)
		}
	}
}
