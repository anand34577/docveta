package documents

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSniff(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want string
	}{
		"pdf":          {[]byte("%PDF-1.7\n..."), MimePDF},
		"pdf-junk":     {append([]byte("garbage header\n"), []byte("%PDF-1.4")...), MimePDF},
		"jpeg":         {[]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, MimeJPEG},
		"png":          {[]byte("\x89PNG\r\n\x1a\n...."), MimePNG},
		"tiff-le":      {[]byte("II*\x00...."), MimeTIFF},
		"tiff-be":      {[]byte("MM\x00*...."), MimeTIFF},
		"webp":         {[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), MimeWebP},
		"heic":         {[]byte("\x00\x00\x00\x18ftypheic\x00\x00"), MimeHEIC},
		"text":         {[]byte("Hello\nबिजली बिल\n"), MimeText},
		"binary":       {[]byte{0, 1, 2, 3, 4, 5, 0, 0}, ""},
		"html-is-text": {[]byte("<html><script>x</script></html>"), MimeText},
		"empty":        {nil, ""},
	}
	for name, c := range cases {
		got, err := Sniff(writeTemp(t, c.data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", name, got, c.want)
		}
	}
}

func TestSniffOffice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doc.docx")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte("<w:document/>"))
	zw.Close()
	f.Close()
	if got, _ := Sniff(p); got != MimeDOCX {
		t.Fatalf("got %q", got)
	}
}

func TestTitleFromFilename(t *testing.T) {
	cases := map[string]string{
		"scan_2026-08-05 BESCOM_bill.pdf": "scan 2026-08-05 BESCOM bill",
		`C:\Users\x\Desktop\passport.jpg`: "passport",
		".pdf":                            "Untitled document",
		"":                                "Untitled document",
	}
	for in, want := range cases {
		if got := TitleFromFilename(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
