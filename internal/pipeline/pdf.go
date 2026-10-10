// Package pipeline orchestrates document processing: preprocessing in the core,
// OCR and archive generation by external workers (pull-based lease protocol),
// merging results, indexing and classification.
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
	"unicode/utf16"

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
		t, err := pageText(inst, requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}})
		if err != nil {
			info.Texts = append(info.Texts, "")
			continue
		}
		info.Texts = append(info.Texts, t)
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

// pageText is a page's embedded text, as PDFium reads it, minus the spaces PDFium makes up
// inside Hindi (and other Indic) words. PDFium inserts a space wherever the next glyph seems
// far from the previous one, and Indic glyphs (ि drawn left of its letter, matras with odd
// widths) fool it: "शि क्षा", "ना म". A made-up space whose letters touch on the page goes.
func pageText(inst pdfium.Pdfium, page requests.Page) (string, error) {
	tp, err := inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: page})
	if err != nil {
		return "", err
	}
	defer inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage}) //nolint:errcheck
	n, err := inst.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: tp.TextPage})
	if err != nil {
		return "", err
	}
	units := make([]uint16, n.Count)
	for i := range units {
		u, err := inst.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{TextPage: tp.TextPage, Index: i})
		if err != nil {
			return "", err
		}
		units[i] = uint16(u.Unicode)
	}
	drop := falseSpaces(units,
		func(i int) bool {
			g, err := inst.FPDFText_IsGenerated(&requests.FPDFText_IsGenerated{TextPage: tp.TextPage, Index: i})
			return err == nil && g.IsGenerated
		},
		func(i int) (charBox, bool) {
			b, err := inst.FPDFText_GetCharBox(&requests.FPDFText_GetCharBox{TextPage: tp.TextPage, Index: i})
			if err != nil || b.Right <= b.Left || b.Top <= b.Bottom {
				return charBox{}, false
			}
			fs, err := inst.FPDFText_GetFontSize(&requests.FPDFText_GetFontSize{TextPage: tp.TextPage, Index: i})
			if err != nil {
				return charBox{}, false
			}
			return charBox{b.Left, b.Right, b.Bottom, b.Top, fs.FontSize}, true
		})
	kept := units[:0]
	for i, u := range units {
		if u != 0 && !drop[i] { // 0: a glyph PDFium couldn't map to a character
			kept = append(kept, u)
		}
	}
	return string(utf16.Decode(kept)), nil
}

type charBox struct{ left, right, bottom, top, size float64 } // size: font size, points

func indic(u uint16) bool { return u >= 0x0900 && u <= 0x0DFF } // Devanagari … Sinhala

// falseSpaces finds generated spaces between two Indic characters where the next glyph starts
// within a tenth of an em of the word before it (measured to the word's rightmost ink, since ि
// sits left of its letter). Glyphs inside a word touch or overlap; a real word gap is about
// 1/6 em of ink even in tight Hindi type (2.6 pt at 16 pt in Chrome's PDFs).
func falseSpaces(units []uint16, generated func(int) bool, box func(int) (charBox, bool)) map[int]bool {
	drop := map[int]bool{}
	for i := 1; i+1 < len(units); i++ {
		if units[i] != ' ' || !indic(units[i-1]) || !indic(units[i+1]) || !generated(i) {
			continue
		}
		next, ok := box(i + 1)
		if !ok {
			continue
		}
		word, found := charBox{right: -1e9, bottom: 1e9, top: -1e9}, false
		for j := i - 1; j >= 0 && !unicode.IsSpace(rune(units[j])); j-- {
			if b, ok := box(j); ok {
				word = charBox{0, max(word.right, b.right), min(word.bottom, b.bottom), max(word.top, b.top), b.size}
				found = true
			}
		}
		// The smaller of font size and line height: a PDF that scales up a 1 pt font gets fewer joins, never wrong ones.
		em := min(next.size, 1.2*max(word.top-word.bottom, next.top-next.bottom))
		sameLine := next.bottom < word.top && next.top > word.bottom
		if found && sameLine && next.left-word.right < 0.1*em {
			drop[i] = true
		}
	}
	return drop
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
	if LegacyIndicText(s) {
		return false // looks like text, reads as nonsense: recognise the page instead
	}
	return letters*100/total >= 50
}

// Characters the Walkman-Chanakya / DV-TT family of pre-Unicode Hindi fonts (used by exam
// boards, government offices, many Indian publishers) store in place of Devanagari. A PDF
// typeset in them has a text layer like "¬⁄UËˇÊÊ ¬ÈÁSÃ∑§Ê" for "परीक्षा पुस्तिका". The set
// leaves out characters ordinary European text uses (ß, ç, é, ’, §, ¿, ﬁ ligatures).
const chanakyaChars = "¬∑◊⁄ˇ∞∏≈∆˝˛‡Ÿÿ‚¥›‹Œ„÷∫¸´ÅÊÈÁËÃÒÙÓÔÛÚÀÕÉ"

// Words Kruti Dev and similar ASCII-mapped Hindi fonts produce for very common Hindi words
// (है, का, के, की, में, से, और, यह, कि, तो, भी, नहीं, पर, ने, को, हो, था, लिए, आप, एक).
var krutiWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("gS gSa dk ds dh esa ls vkSj ;g ;s fd rks Hkh ugha ij us dks gks Fkk FkkA fy, vki ,d djsa djuk tks bl ml") {
		krutiWords[w] = true
	}
}

// LegacyIndicText reports whether text comes from a pre-Unicode Hindi font: it looks like
// letters and symbols but isn't readable, so search and Ask would see nonsense.
func LegacyIndicText(s string) bool {
	nonSpace, odd, oddLines := 0, 0, 0
	for _, line := range strings.Split(s, "\n") {
		lineChars, lineOdd := 0, 0
		for _, r := range line {
			if unicode.IsSpace(r) {
				continue
			}
			lineChars++
			if strings.ContainsRune(chanakyaChars, r) {
				lineOdd++
			}
		}
		nonSpace += lineChars
		odd += lineOdd
		if lineOdd >= 3 && lineOdd*5 >= lineChars { // a fifth of the line
			oddLines++
		}
	}
	if odd >= 12 && (odd*100 >= nonSpace*3 || oddLines >= 3) {
		return true
	}
	words, kruti := 0, 0
	for _, w := range strings.Fields(s) {
		words++
		if krutiWords[strings.TrimRight(w, ".,")] || krutiWords[w] {
			kruti++
		}
	}
	return kruti >= 6 && kruti*100 >= words*8
}
