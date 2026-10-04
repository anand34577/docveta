package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// testPDF builds a one-page PDF containing text.
func testPDF(text string) []byte {
	var b bytes.Buffer
	var off []int
	obj := func(s string) {
		off = append(off, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(off), s)
	}
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [4 0 R] /Count 1 >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents 5 0 R >>")
	stream := fmt.Sprintf("BT /F1 14 Tf 72 760 Td (%s) Tj ET", text)
	obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(off)+1)
	for _, o := range off {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(off)+1, x)
	return b.Bytes()
}

func src(b []byte) PDFSource { return PDFSource{R: bytes.NewReader(b), Size: int64(len(b))} }

func pageTexts(t *testing.T, p *PDF, b []byte) []string {
	t.Helper()
	info, err := p.Inspect(bytes.NewReader(b), int64(len(b)), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(info.Texts))
	for i, s := range info.Texts {
		out[i] = strings.TrimSpace(s)
	}
	return out
}

func TestPDFBuildMergeSplitRotate(t *testing.T) {
	p, err := NewPDF(1)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a, b := testPDF("alpha"), testPDF("beta")

	// Merge two documents, then take pages in a new order.
	merged, err := p.Build([]PDFSource{src(a), src(b)}, []PageRef{{0, 1, 0}, {1, 1, 0}, {0, 1, 0}})
	if err != nil {
		t.Fatal(err)
	}
	got := pageTexts(t, p, merged)
	if strings.Join(got, ",") != "alpha,beta,alpha" {
		t.Fatalf("merged text: %v", got)
	}

	// Reorder + delete + rotate (page 2 dropped, page 3 first and turned 90°).
	out, err := p.Build([]PDFSource{src(merged)}, []PageRef{{0, 3, 90}, {0, 1, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if got := pageTexts(t, p, out); strings.Join(got, ",") != "alpha,alpha" {
		t.Fatalf("edited text: %v", got)
	}
	if n, err := p.PageCountOf(src(out)); err != nil || n != 2 {
		t.Fatalf("page count %d %v", n, err)
	}

	// Bad input is rejected, not turned into a corrupt file.
	if _, err := p.Build([]PDFSource{src(a)}, []PageRef{{0, 5, 0}}); err == nil {
		t.Error("expected an error for a page that doesn't exist")
	}
	if _, err := p.Build([]PDFSource{src(a)}, []PageRef{{0, 1, 45}}); err == nil {
		t.Error("expected an error for a rotation that isn't a multiple of 90")
	}
	if _, err := p.Build([]PDFSource{src(a)}, nil); err == nil {
		t.Error("expected an error when no pages are selected")
	}

	// Unlock of a plain PDF still returns a readable copy.
	u, err := p.Unlock(src(a))
	if err != nil {
		t.Fatal(err)
	}
	if got := pageTexts(t, p, u); strings.Join(got, ",") != "alpha" {
		t.Fatalf("unlocked text: %v", got)
	}
}

func TestPDFUnlockEncrypted(t *testing.T) {
	enc, err := os.ReadFile("testdata/encrypted.pdf") // password: hunter2-test
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPDF(1)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// Without the password it is reported as password protected.
	if _, err := p.Inspect(bytes.NewReader(enc), int64(len(enc)), "", 10); !errors.Is(err, ErrPassword) {
		t.Fatalf("expected ErrPassword, got %v", err)
	}
	// A wrong password is a clear error, not a corrupt result.
	if _, err := p.Unlock(PDFSource{R: bytes.NewReader(enc), Size: int64(len(enc)), Password: "wrong"}); !errors.Is(err, ErrPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	out, err := p.Unlock(PDFSource{R: bytes.NewReader(enc), Size: int64(len(enc)), Password: "hunter2-test"})
	if err != nil {
		t.Fatal(err)
	}
	// The unlocked copy opens without a password and keeps its text.
	if got := pageTexts(t, p, out); strings.Join(got, ",") != "secret bank statement for october 2026" {
		t.Fatalf("unlocked text: %v", got)
	}
}
