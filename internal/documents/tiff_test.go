package documents

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTIFF makes a TIFF whose directories each hold one dummy entry, chained n deep.
func buildTIFF(bo binary.ByteOrder, n int, loop bool) []byte {
	var b bytes.Buffer
	if bo == binary.LittleEndian {
		b.WriteString("II")
	} else {
		b.WriteString("MM")
	}
	binary.Write(&b, bo, uint16(42))
	binary.Write(&b, bo, uint32(8))
	for i := 0; i < n; i++ {
		binary.Write(&b, bo, uint16(1))
		b.Write(make([]byte, 12))
		next := uint32(b.Len() + 4)
		if i == n-1 {
			next = 0
			if loop {
				next = 8
			}
		}
		binary.Write(&b, bo, next)
	}
	return b.Bytes()
}

func TestTIFFPages(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, n := range []int{1, 3, 12} {
			b := buildTIFF(bo, n, false)
			got, err := TIFFPages(bytes.NewReader(b), int64(len(b)))
			if err != nil || got != n {
				t.Errorf("%v n=%d: got %d, %v", bo, n, got, err)
			}
		}
	}
	// A directory chain that loops back must terminate.
	b := buildTIFF(binary.LittleEndian, 3, true)
	if got, err := TIFFPages(bytes.NewReader(b), int64(len(b))); err != nil || got != 3 {
		t.Errorf("loop: got %d, %v", got, err)
	}
	if _, err := TIFFPages(bytes.NewReader([]byte("not a tiff at all")), 17); err == nil {
		t.Error("expected an error for non-TIFF data")
	}
}
