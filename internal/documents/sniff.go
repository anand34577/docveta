package documents

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Supported MIME types and how they are processed.
const (
	MimePDF  = "application/pdf"
	MimeJPEG = "image/jpeg"
	MimePNG  = "image/png"
	MimeTIFF = "image/tiff"
	MimeWebP = "image/webp"
	MimeGIF  = "image/gif"
	MimeBMP  = "image/bmp"
	MimeHEIC = "image/heic"
	MimeAVIF = "image/avif"
	MimeText = "text/plain"
	MimeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	MimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	MimePPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	MimeODT  = "application/vnd.oasis.opendocument.text"
	MimeODS  = "application/vnd.oasis.opendocument.spreadsheet"
	MimeODP  = "application/vnd.oasis.opendocument.presentation"
)

// IsImage reports whether the core can decode the image itself.
func IsImage(m string) bool {
	switch m {
	case MimeJPEG, MimePNG, MimeTIFF, MimeWebP, MimeGIF, MimeBMP:
		return true
	}
	return false
}

// NeedsWorkerConversion reports formats the core can't render and that a worker with
// the "convert" capability turns into PDF.
func NeedsWorkerConversion(m string) bool { return m == MimeHEIC || m == MimeAVIF }

// IsOffice reports office formats converted via Gotenberg.
func IsOffice(m string) bool {
	switch m {
	case MimeDOCX, MimeXLSX, MimePPTX, MimeODT, MimeODS, MimeODP:
		return true
	}
	return false
}

// Sniff detects the MIME type from file content. Extensions and client headers are
// never trusted. Returns "" for unsupported content.
func Sniff(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	head = head[:n]
	switch {
	case n == 0:
		return "", nil
	case bytes.Contains(head[:min(n, 1024)], []byte("%PDF-")):
		return MimePDF, nil
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return MimeJPEG, nil
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return MimePNG, nil
	case bytes.HasPrefix(head, []byte("II*\x00")), bytes.HasPrefix(head, []byte("MM\x00*")):
		return MimeTIFF, nil
	case n >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && string(head[8:12]) == "WEBP":
		return MimeWebP, nil
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return MimeGIF, nil
	case bytes.HasPrefix(head, []byte("BM")) && n > 26:
		return MimeBMP, nil
	case n >= 12 && string(head[4:8]) == "ftyp":
		switch string(head[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return MimeHEIC, nil
		case "avif", "avis":
			return MimeAVIF, nil
		}
		return "", nil
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		st, err := f.Stat()
		if err != nil {
			return "", err
		}
		return sniffZip(f, st.Size()), nil
	}
	if looksLikeText(head) {
		return MimeText, nil
	}
	return "", nil
}

func looksLikeText(b []byte) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	// Allow a truncated multi-byte sequence at the end of the sample.
	for i := 0; i < 4 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	if !utf8.Valid(b) {
		return false
	}
	ctrl := 0
	for _, c := range b {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' && c != '\f' {
			ctrl++
		}
	}
	return ctrl*100 < len(b)+1
}

func sniffZip(r io.ReaderAt, size int64) string {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return MimeDOCX
		case f.Name == "xl/workbook.xml":
			return MimeXLSX
		case f.Name == "ppt/presentation.xml":
			return MimePPTX
		case f.Name == "mimetype":
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			b, _ := io.ReadAll(io.LimitReader(rc, 100))
			rc.Close()
			switch strings.TrimSpace(string(b)) {
			case MimeODT:
				return MimeODT
			case MimeODS:
				return MimeODS
			case MimeODP:
				return MimeODP
			}
		}
	}
	return ""
}

// ExtFor returns a file extension for a MIME type (used for download filenames).
func ExtFor(m string) string {
	switch m {
	case MimePDF:
		return ".pdf"
	case MimeJPEG:
		return ".jpg"
	case MimePNG:
		return ".png"
	case MimeTIFF:
		return ".tiff"
	case MimeWebP:
		return ".webp"
	case MimeGIF:
		return ".gif"
	case MimeBMP:
		return ".bmp"
	case MimeHEIC:
		return ".heic"
	case MimeAVIF:
		return ".avif"
	case MimeText:
		return ".txt"
	case MimeDOCX:
		return ".docx"
	case MimeXLSX:
		return ".xlsx"
	case MimePPTX:
		return ".pptx"
	case MimeODT:
		return ".odt"
	case MimeODS:
		return ".ods"
	case MimeODP:
		return ".odp"
	}
	return ""
}
