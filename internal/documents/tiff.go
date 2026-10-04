package documents

import (
	"encoding/binary"
	"errors"
	"io"
)

// TIFFPages counts the images (pages) in a TIFF by walking its chain of image file
// directories. golang.org/x/image/tiff decodes only the first page and can't count.
// Each directory costs one small read, so a hostile file can't make this expensive: the
// walk stops at maxPages and at any offset that loops back.
func TIFFPages(r io.ReaderAt, size int64) (int, error) {
	const maxPages = 5000
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return 0, err
	}
	var bo binary.ByteOrder
	switch string(hdr[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, errors.New("not a TIFF")
	}
	big := false
	next := int64(bo.Uint32(hdr[4:8]))
	switch bo.Uint16(hdr[2:4]) {
	case 42:
	case 43: // BigTIFF: 8-byte offsets
		big = true
		var h2 [16]byte
		if _, err := r.ReadAt(h2[:], 0); err != nil {
			return 0, err
		}
		next = int64(bo.Uint64(h2[8:16]))
	default:
		return 0, errors.New("not a TIFF")
	}
	seen := map[int64]bool{}
	pages := 0
	for next > 0 && next < size && !seen[next] && pages < maxPages {
		seen[next] = true
		var count int64
		var entry int64 = 12
		var nextLen int64 = 4
		if big {
			var b [8]byte
			if _, err := r.ReadAt(b[:], next); err != nil {
				break
			}
			count = int64(bo.Uint64(b[:]))
			entry, nextLen = 20, 8
			next += 8
		} else {
			var b [2]byte
			if _, err := r.ReadAt(b[:], next); err != nil {
				break
			}
			count = int64(bo.Uint16(b[:]))
			next += 2
		}
		pos := next + count*entry
		var nb [8]byte
		if _, err := r.ReadAt(nb[:nextLen], pos); err != nil {
			pages++ // a truncated last directory still counts as a page
			break
		}
		pages++
		if big {
			next = int64(bo.Uint64(nb[:]))
		} else {
			next = int64(bo.Uint32(nb[:4]))
		}
	}
	if pages == 0 {
		return 0, errors.New("no TIFF directories")
	}
	return pages, nil
}
