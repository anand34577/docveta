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
// WebAssembly, so no C library is needed and decoding runs sandboxed.
func decodeModern(r io.Reader, mime string) (image.Image, error) {
	switch mime {
	case documents.MimeHEIC:
		return heic.Decode(r)
	case documents.MimeAVIF:
		return avif.Decode(r)
	}
	return nil, fmt.Errorf("not a modern image format: %s", mime)
}

// modernToJPEG converts a HEIC/AVIF original into a JPEG that browsers and every OCR
// engine understand. The original is kept untouched.
func modernToJPEG(r io.Reader, mime string) (img image.Image, jpg []byte, err error) {
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
