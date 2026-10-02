package pipeline

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
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
