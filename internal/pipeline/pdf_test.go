package pipeline

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
)

// minimalPDF builds a valid PDF with one text line per page (correct xref offsets).
func minimalPDF(pages []string) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	n := len(pages)
	kids := make([]string, n)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i*2)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, text := range pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+i*2))
		stream := fmt.Sprintf("BT /F1 18 Tf 72 760 Td (%s) Tj ET", text)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

func TestPDFInspect(t *testing.T) {
	if testing.Short() {
		t.Skip("pdfium wasm init is slow")
	}
	p, err := NewPDF(1)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	data := minimalPDF([]string{"Electricity bill from BESCOM for August 2026", "Second page has more text here for testing"})
	info, err := p.Inspect(bytes.NewReader(data), int64(len(data)), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if info.PageCount != 2 {
		t.Fatalf("pages %d", info.PageCount)
	}
	if !strings.Contains(info.Texts[0], "BESCOM") || !HasUsableText(info.Texts[1]) {
		t.Fatalf("texts %q", info.Texts)
	}
	if info.Thumbnail == nil {
		t.Fatal("no thumbnail")
	}
	jpg, w, h, err := Thumbnail(info.Thumbnail, 1)
	if err != nil || len(jpg) < 100 || w != thumbWidth || h <= w {
		t.Fatalf("thumbnail %d bytes %dx%d %v", len(jpg), w, h, err)
	}
	if _, err := p.Inspect(bytes.NewReader([]byte("%PDF-1.4 broken")), 15, "", 10); err == nil {
		t.Fatal("broken PDF should fail")
	}
}

func TestFalseSpacesInIndicWords(t *testing.T) {
	// "शि क्षा का" as PDFium reads it: spaces after शि and क्षा are both made up. The ि glyph is
	// drawn left of श, so "शि" seems to end far left of क्ष; क्षा really is followed by a word gap.
	text := []rune("शि क्षा का")
	units := utf16.Encode(text)
	boxes := map[int]charBox{
		0: {10, 20, 0, 10, 16}, 1: {6, 11, 0, 12, 16}, // श, ि (left of श)
		3: {21, 31, 0, 10, 16}, 4: {30, 34, 0, 10, 16}, 5: {33, 38, 0, 10, 16}, 6: {37, 40, 0, 10, 16}, // क ् ष ा
		8: {44, 52, 0, 10, 16}, 9: {51, 54, 0, 10, 16}, // क ा: 4 units after the previous word
	}
	box := func(i int) (charBox, bool) { b, ok := boxes[i]; return b, ok }
	got := falseSpaces(units, func(int) bool { return true }, box)
	if !got[2] || got[7] || len(got) != 1 {
		t.Fatalf("dropped %v, want only the space after शि", got)
	}
	if got := falseSpaces(units, func(int) bool { return false }, box); len(got) != 0 {
		t.Fatalf("real spaces must stay: %v", got)
	}
	// A made-up space before a glyph on the next line is a line break, not a false space.
	boxes[3], boxes[4], boxes[5], boxes[6] = charBox{21, 31, 20, 30, 16}, charBox{30, 34, 20, 30, 16}, charBox{33, 38, 20, 30, 16}, charBox{37, 40, 20, 30, 16}
	if got := falseSpaces(units, func(int) bool { return true }, box); got[2] {
		t.Fatalf("line break joined: %v", got)
	}
}
