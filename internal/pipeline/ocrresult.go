package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// OCRResult is the canonical, engine-independent result format "ocr-result/v1".
// Every worker returns this; words/boxes are optional.
type OCRResult struct {
	Schema  string         `json:"schema"`
	Engine  OCREngine      `json:"engine"`
	Pages   []OCRPage      `json:"pages"`
	Metrics map[string]any `json:"metrics,omitempty"`
}

type OCREngine struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Models  map[string]string `json:"models,omitempty"`
}

type OCRPage struct {
	Page       int        `json:"page"`
	Width      float64    `json:"width,omitempty"`
	Height     float64    `json:"height,omitempty"`
	Unit       string     `json:"unit,omitempty"`
	DPI        float64    `json:"dpi,omitempty"`
	Rotation   int        `json:"rotation"`
	Language   string     `json:"language,omitempty"`
	Confidence *float64   `json:"confidence,omitempty"`
	Text       string     `json:"text"`
	Blocks     []OCRBlock `json:"blocks,omitempty"`
}

type OCRBlock struct {
	BBox  []float64 `json:"bbox,omitempty"`
	Type  string    `json:"type,omitempty"`
	Lines []OCRLine `json:"lines,omitempty"`
}

type OCRLine struct {
	BBox       []float64 `json:"bbox,omitempty"`
	Text       string    `json:"text"`
	Confidence *float64  `json:"confidence,omitempty"`
	Words      []OCRWord `json:"words,omitempty"`
}

type OCRWord struct {
	BBox       []float64 `json:"bbox"`
	Text       string    `json:"text"`
	Confidence *float64  `json:"confidence,omitempty"`
}

const SchemaV1 = "ocr-result/v1"

// ParseOCRResult validates a worker result. pageFrom/pageTo (1-based, inclusive; 0 =
// unknown) bound the pages a task may report.
func ParseOCRResult(raw []byte, pageFrom, pageTo int) (*OCRResult, error) {
	var r OCRResult
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("result is not valid JSON: %w", err)
	}
	if r.Schema != SchemaV1 {
		return nil, fmt.Errorf("unsupported result schema %q (want %q)", r.Schema, SchemaV1)
	}
	if r.Engine.Name == "" {
		return nil, errors.New("engine.name is required")
	}
	seen := map[int]bool{}
	for i := range r.Pages {
		p := &r.Pages[i]
		if p.Page < 1 {
			return nil, fmt.Errorf("pages[%d].page must be ≥ 1", i)
		}
		if pageFrom > 0 && (p.Page < pageFrom || p.Page > pageTo) {
			return nil, fmt.Errorf("pages[%d].page=%d is outside the task's range %d–%d", i, p.Page, pageFrom, pageTo)
		}
		if seen[p.Page] {
			return nil, fmt.Errorf("page %d reported twice", p.Page)
		}
		seen[p.Page] = true
		switch p.Rotation {
		case 0, 90, 180, 270:
		default:
			return nil, fmt.Errorf("pages[%d].rotation must be 0, 90, 180 or 270", i)
		}
		if p.Text == "" {
			p.Text = p.textFromLines()
		}
	}
	sort.Slice(r.Pages, func(a, b int) bool { return r.Pages[a].Page < r.Pages[b].Page })
	return &r, nil
}

func (p *OCRPage) textFromLines() string {
	var sb strings.Builder
	for _, b := range p.Blocks {
		for _, l := range b.Lines {
			sb.WriteString(l.Text)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

// HasBoxes reports whether word-level coordinates are present (needed for a text layer).
func (r *OCRResult) HasBoxes() bool {
	for _, p := range r.Pages {
		for _, b := range p.Blocks {
			for _, l := range b.Lines {
				if len(l.Words) > 0 && len(l.Words[0].BBox) == 4 {
					return true
				}
			}
		}
	}
	return false
}
