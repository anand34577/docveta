package pipeline

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"

	"github.com/anand34577/docveta/internal/documents"
)

// decodeModern decodes HEIC/HEIF (iPhone photos) and AVIF. Both decoders ship as
// WebAssembly, so no C library is needed and decoding runs sandboxed. Like DecodeImage it
// refuses decompression bombs: the dimensions are read before any pixels.
func decodeModern(r io.ReadSeeker, mime string) (image.Image, error) {
	var config func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	switch mime {
	case documents.MimeHEIC:
		config, decode = heic.DecodeConfig, heic.Decode
	case documents.MimeAVIF:
		config, decode = avif.DecodeConfig, func(r io.Reader) (image.Image, error) { return avif.Decode(r) }
	default:
		return nil, fmt.Errorf("not a modern image format: %s", mime)
	}
	cfg, err := config(r)
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, errTooManyPixels
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return decode(r)
}

// modernToJPEG converts a HEIC/AVIF original into a JPEG that browsers and every OCR
// engine understand. The original is kept untouched.
func modernToJPEG(r io.ReadSeeker, mime string) (img image.Image, jpg []byte, err error) {
	img, err = decodeModern(r, mime)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenWhite(img), &jpeg.Options{Quality: 92}); err != nil {
		return nil, nil, err
	}
	return img, buf.Bytes(), nil
}
