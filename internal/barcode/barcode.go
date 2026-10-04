// Package barcode reads barcodes from page images (separator sheets and archive-number
// labels on scanned batches) and draws the sheets and labels to print.
package barcode

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"regexp"
	"strconv"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// SeparatorText is what a separator sheet's barcode says.
const SeparatorText = "DOCVETA-SEPARATOR"

// legacySeparator is the code many scanners and paperless-ngx users already print.
const legacySeparator = "PATCHT"

// Decode returns the text of every barcode (Code 128 or QR) found on an image.
func Decode(img image.Image) []string {
	bmp, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(gozxing.NewLuminanceSourceFromImage(img)))
	if err != nil {
		return nil
	}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	var out []string
	for _, r := range []gozxing.Reader{oned.NewCode128Reader(), qrcode.NewQRCodeReader()} {
		if res, err := r.Decode(bmp, hints); err == nil && res.GetText() != "" {
			out = append(out, res.GetText())
		}
	}
	return out
}

// IsSeparator reports whether a barcode text marks a separator sheet.
func IsSeparator(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	return s == SeparatorText || s == legacySeparator
}

var asnRe = regexp.MustCompile(`^(?i)ASN\s*0*(\d{1,12})$`)

// ASN extracts an archive serial number from a label barcode such as "ASN00042".
func ASN(s string) (int64, bool) {
	m := asnRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return n, err == nil && n > 0
}

// ASNText is the barcode text for an archive number.
func ASNText(n int64) string { return fmt.Sprintf("ASN%05d", n) }

// code128 renders text as a Code 128 barcode, scale pixels per module, height pixels tall.
func code128(text string, scale, height int) (image.Image, error) {
	w := oned.NewCode128Writer()
	m, err := w.Encode(text, gozxing.BarcodeFormat_CODE_128, modules(text)*scale, height, nil)
	if err != nil {
		return nil, err
	}
	img := image.NewGray(image.Rect(0, 0, m.GetWidth(), m.GetHeight()))
	for y := 0; y < m.GetHeight(); y++ {
		for x := 0; x < m.GetWidth(); x++ {
			if m.Get(x, y) {
				img.SetGray(x, y, color.Gray{0})
			} else {
				img.SetGray(x, y, color.Gray{255})
			}
		}
	}
	return img, nil
}

// modules estimates the number of Code 128 modules for text so the barcode gets the right width.
func modules(text string) int { return (len(text)+3)*11 + 35 }

// drawText writes a caption with the built-in font, enlarged by an integer factor.
func drawText(dst *image.Gray, text string, x, y, scale int) {
	face := basicfont.Face7x13
	src := image.NewGray(image.Rect(0, 0, len(text)*7+2, 16))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.Gray{255}), image.Point{}, draw.Src)
	d := font.Drawer{Dst: src, Src: image.NewUniform(color.Gray{0}), Face: face, Dot: fixed.P(1, 12)}
	d.DrawString(text)
	for sy := 0; sy < src.Bounds().Dy()*scale; sy++ {
		for sx := 0; sx < src.Bounds().Dx()*scale; sx++ {
			if px, py := x+sx, y+sy; image.Pt(px, py).In(dst.Bounds()) {
				dst.SetGray(px, py, src.GrayAt(sx/scale, sy/scale))
			}
		}
	}
}

func canvas(w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{255}), image.Point{}, draw.Src)
	return img
}

// SeparatorSheet draws a printable page (about A4 at 150 dpi) with the separator barcode.
func SeparatorSheet() (image.Image, error) {
	bc, err := code128(SeparatorText, 5, 360)
	if err != nil {
		return nil, err
	}
	page := canvas(1240, 1754)
	off := image.Pt((1240-bc.Bounds().Dx())/2, 520)
	draw.Draw(page, bc.Bounds().Add(off), bc, image.Point{}, draw.Src)
	drawText(page, "DOCVETA SEPARATOR PAGE", 200, 300, 6)
	drawText(page, "Put this sheet between two documents in a batch scan.", 200, 460, 3)
	drawText(page, "Docveta splits the scan here and leaves this sheet out.", 200, 960, 3)
	return page, nil
}

// ASNLabel draws a label for an archive number to stick on a paper original.
func ASNLabel(n int64) (image.Image, error) {
	text := ASNText(n)
	bc, err := code128(text, 3, 110)
	if err != nil {
		return nil, err
	}
	w := bc.Bounds().Dx() + 60
	l := canvas(w, 200)
	draw.Draw(l, bc.Bounds().Add(image.Pt(30, 20)), bc, image.Point{}, draw.Src)
	drawText(l, text, 30, 145, 3)
	return l, nil
}

// QRDataURL draws text as a QR code and returns it as a PNG data URL (for authenticator-app setup).
func QRDataURL(text string, size int) (string, error) {
	m, err := qrcode.NewQRCodeWriter().Encode(text, gozxing.BarcodeFormat_QR_CODE, size, size, nil)
	if err != nil {
		return "", err
	}
	img := canvas(m.GetWidth(), m.GetHeight())
	for y := 0; y < m.GetHeight(); y++ {
		for x := 0; x < m.GetWidth(); x++ {
			if m.Get(x, y) {
				img.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
