// Package pipeline orchestrates document processing: preprocessing in the core,
// OCR and archive generation by external workers (pull-based lease protocol),
// merging results, indexing and classification (DESIGN §10, §11).
package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/klippa-app/go-pdfium"
	pdferrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"golang.org/x/image/draw"
)

// PDF wraps a pool of sandboxed PDFium (WebAssembly) instances. Parsing untrusted PDFs
// inside a WASM VM means a malicious file can't compromise the server process.
type PDF struct {
	pool pdfium.Pool
}

func NewPDF(workers int) (*PDF, error) {
	if workers < 1 {
		workers = 1
	}
	pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: workers, MaxTotal: workers})
	if err != nil {
		return nil, fmt.Errorf("init pdfium: %w", err)
	}
	return &PDF{pool: pool}, nil
}

func (p *PDF) Close() error { return p.pool.Close() }

var ErrPassword = errors.New("pdf is password protected")

// PDFInfo is what preprocessing needs from a PDF.
type PDFInfo struct {
	PageCount int
	Texts     []string    // embedded text per page
	Thumbnail image.Image // first page, nil if rendering failed
}

const thumbWidth = 480

// Inspect opens a PDF and extracts page count, per-page embedded text and a thumbnail.
// The file is streamed into PDFium rather than loaded fully into memory.
func (p *PDF) Inspect(r io.ReadSeeker, size int64, password string, maxTextPages int) (*PDFInfo, error) {
	inst, err := p.pool.GetInstance(2 * time.Minute)
	if err != nil {
		return nil, fmt.Errorf("pdfium instance: %w", err)
	}
	defer inst.Close()

	req := &requests.OpenDocument{FileReader: r, FileReaderSize: size}
	if password != "" {
		req.Password = &password
	}
	doc, err := inst.OpenDocument(req)
	if err != nil {
		if errors.Is(err, pdferrors.ErrPassword) {
			return nil, ErrPassword
		}
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck

	pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("page count: %w", err)
	}
	info := &PDFInfo{PageCount: pc.PageCount}
	for i := 0; i < pc.PageCount; i++ {
		if i >= maxTextPages {
			info.Texts = append(info.Texts, "")
			continue
		}
		t, err := inst.GetPageText(&requests.GetPageText{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}}})
		if err != nil {
			info.Texts = append(info.Texts, "")
			continue
		}
		info.Texts = append(info.Texts, t.Text)
	}
	if pc.PageCount > 0 {
		r, err := inst.RenderPageInPixels(&requests.RenderPageInPixels{
			Page:  requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0}},
			Width: thumbWidth, Height: thumbWidth * 2,
		})
		if err == nil {
			// The WASM pixel buffer is only valid until Cleanup; copy it out.
			src := r.Result.RenderedImage
			dst := image.NewRGBA(src.Bounds())
			draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
			r.Cleanup()
			info.Thumbnail = flattenWhite(dst)
		}
	}
	return info, nil
}

// flattenWhite composites a possibly transparent page onto white.
func flattenWhite(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, image.White, image.Point{}, draw.Src)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

// Thumbnail scales img to thumbWidth (keeping aspect, max height 2×width), applies the
// EXIF orientation and encodes JPEG.
func Thumbnail(img image.Image, orientation int) ([]byte, int, int, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("empty image")
	}
	srcH := min(h, 2*w) // very tall images: thumbnail the top part
	tw := min(thumbWidth, w)
	th := max(1, srcH*tw/w)
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, image.Rect(b.Min.X, b.Min.Y, b.Min.X+w, b.Min.Y+srcH), draw.Over, nil)
	out := orient(dst, orientation)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 82}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), out.Bounds().Dx(), out.Bounds().Dy(), nil
}

// HasUsableText decides whether embedded PDF text is real text (not empty, not garbage
// from broken font encodings) so OCR can be skipped for that page.
func HasUsableText(s string) bool {
	s = strings.TrimSpace(s)
	if len([]rune(s)) < 20 {
		return false
	}
	var letters, total, replacement int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		switch {
		case r == unicode.ReplacementChar:
			replacement++
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			letters++
		}
	}
	if total == 0 || replacement*20 > total {
		return false
	}
	return letters*100/total >= 50
}
