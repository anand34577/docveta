package pipeline

import (
	"strings"
	"testing"
)

func TestParseOCRResult(t *testing.T) {
	ok := `{"schema":"ocr-result/v1","engine":{"name":"x","version":"1"},"pages":[
		{"page":3,"rotation":0,"text":"","blocks":[{"lines":[{"text":"hello","words":[{"bbox":[1,2,3,4],"text":"hello"}]}]}]},
		{"page":2,"rotation":90,"text":"two"}]}`
	r, err := ParseOCRResult([]byte(ok), 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages[0].Page != 2 || r.Pages[1].Text != "hello" {
		t.Errorf("pages not sorted / text not derived: %+v", r.Pages)
	}
	if !r.HasBoxes() {
		t.Error("expected boxes")
	}
	bad := map[string]string{
		"schema":   `{"schema":"v0","engine":{"name":"x"},"pages":[]}`,
		"engine":   `{"schema":"ocr-result/v1","engine":{},"pages":[]}`,
		"range":    `{"schema":"ocr-result/v1","engine":{"name":"x"},"pages":[{"page":9,"rotation":0}]}`,
		"dup":      `{"schema":"ocr-result/v1","engine":{"name":"x"},"pages":[{"page":2,"rotation":0},{"page":2,"rotation":0}]}`,
		"rotation": `{"schema":"ocr-result/v1","engine":{"name":"x"},"pages":[{"page":2,"rotation":45}]}`,
		"json":     `{"schema":`,
	}
	for name, raw := range bad {
		if _, err := ParseOCRResult([]byte(raw), 2, 3); err == nil {
			t.Errorf("%s: expected error", name)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%s: empty error", name)
		}
	}
}
