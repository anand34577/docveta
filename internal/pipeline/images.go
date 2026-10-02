package pipeline

import (
	"bufio"
	"encoding/binary"
	"image"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	_ "image/png"
	"io"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// DecodeImage decodes an image and returns its JPEG EXIF orientation (1 = upright).
// Only the first frame of multi-page TIFFs is decoded (workers OCR all pages).
// Orientation is applied to the small thumbnail, not the full image, for speed.
func DecodeImage(r io.ReadSeeker) (image.Image, int, error) {
	orientation := 1
	if o, err := exifOrientation(r); err == nil {
		orientation = o
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, 1, err
	}
	// Refuse decompression bombs: check dimensions before decoding pixels.
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return nil, 1, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > 120_000_000 {
		return nil, 1, errTooManyPixels
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, 1, err
	}
	img, _, err := image.Decode(bufio.NewReader(r))
	return img, orientation, err
}

type imgErr string

func (e imgErr) Error() string { return string(e) }

const errTooManyPixels = imgErr("image is too large to process")

// exifOrientation reads the EXIF orientation tag (0x0112) from a JPEG.
func exifOrientation(r io.ReadSeeker) (int, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 1, err
	}
	br := bufio.NewReader(r)
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return 1, imgErr("not jpeg")
	}
	for i := 0; i < 64; i++ {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return 1, err
		}
		if hdr[0] != 0xFF {
			return 1, imgErr("bad marker")
		}
		marker := hdr[1]
		size := int(binary.BigEndian.Uint16(hdr[2:])) - 2
		if size < 0 {
			return 1, imgErr("bad segment")
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end: no EXIF found
			return 1, nil
		}
		seg := make([]byte, size)
		if _, err := io.ReadFull(br, seg); err != nil {
			return 1, err
		}
		if marker != 0xE1 || len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := seg[6:]
		var bo binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			bo = binary.LittleEndian
		case "MM":
			bo = binary.BigEndian
		default:
			return 1, imgErr("bad tiff header")
		}
		off := int(bo.Uint32(tiff[4:8]))
		if off+2 > len(tiff) {
			return 1, imgErr("bad ifd")
		}
		n := int(bo.Uint16(tiff[off:]))
		for e := 0; e < n; e++ {
			p := off + 2 + e*12
			if p+12 > len(tiff) {
				break
			}
			if bo.Uint16(tiff[p:]) == 0x0112 {
				v := int(bo.Uint16(tiff[p+8:]))
				if v >= 1 && v <= 8 {
					return v, nil
				}
			}
		}
		return 1, nil
	}
	return 1, nil
}

// orient applies an EXIF orientation transform.
func orient(src image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.RGBA
	if o >= 5 {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
