package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/klippa-app/go-pdfium/enums"
	pdferrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
)

// PDFSource is one input PDF for Build. Password is only used to open it in memory.
type PDFSource struct {
	R        io.ReadSeeker
	Size     int64
	Password string
}

// PageRef picks one page of one source for the output.
type PageRef struct {
	Source int // index into the sources passed to Build
	Page   int // 1-based page number in that source
	Rotate int // degrees clockwise to add: 0, 90, 180 or 270
}

func rotation(deg int) (enums.FPDF_PAGE_ROTATION, error) {
	switch ((deg % 360) + 360) % 360 {
	case 0:
		return enums.FPDF_PAGE_ROTATION_NONE, nil
	case 90:
		return enums.FPDF_PAGE_ROTATION_90_CW, nil
	case 180:
		return enums.FPDF_PAGE_ROTATION_180_CW, nil
	case 270:
		return enums.FPDF_PAGE_ROTATION_270_CW, nil
	}
	return 0, fmt.Errorf("rotation must be a multiple of 90, got %d", deg)
}

// PageCountOf returns the number of pages of a PDF (password only needed for encrypted files).
func (p *PDF) PageCountOf(s PDFSource) (int, error) {
	inst, err := p.pool.GetInstance(2 * time.Minute)
	if err != nil {
		return 0, err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(openReq(s))
	if err != nil {
		return 0, mapOpenErr(err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck
	pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return 0, err
	}
	return pc.PageCount, nil
}

func openReq(s PDFSource) *requests.OpenDocument {
	req := &requests.OpenDocument{FileReader: s.R, FileReaderSize: s.Size}
	if s.Password != "" {
		pw := s.Password
		req.Password = &pw
	}
	return req
}

func mapOpenErr(err error) error {
	if errors.Is(err, pdferrors.ErrPassword) {
		return ErrPassword
	}
	return fmt.Errorf("open pdf: %w", err)
}

// Unlock opens an encrypted PDF with its password and returns a copy without
// encryption. The password is used only in memory and never stored.
func (p *PDF) Unlock(s PDFSource) ([]byte, error) {
	inst, err := p.pool.GetInstance(2 * time.Minute)
	if err != nil {
		return nil, err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(openReq(s))
	if err != nil {
		return nil, mapOpenErr(err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck
	var buf bytes.Buffer
	if _, err := inst.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{Document: doc.Document, Flags: requests.SaveFlagRemoveSecurity, FileWriter: &buf}); err != nil {
		return nil, fmt.Errorf("save unlocked pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// Build assembles a new PDF from the listed pages of the sources, in order, applying
// rotations. Rotate/delete/reorder pages, split and merge are all this one operation.
func (p *PDF) Build(srcs []PDFSource, pages []PageRef) ([]byte, error) {
	if len(pages) == 0 {
		return nil, errors.New("no pages selected")
	}
	inst, err := p.pool.GetInstance(5 * time.Minute)
	if err != nil {
		return nil, err
	}
	defer inst.Close()

	docs := make([]references.FPDF_DOCUMENT, len(srcs))
	counts := make([]int, len(srcs))
	for i, s := range srcs {
		d, err := inst.OpenDocument(openReq(s))
		if err != nil {
			return nil, mapOpenErr(err)
		}
		docs[i] = d.Document
		defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.Document}) //nolint:errcheck
		pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.Document})
		if err != nil {
			return nil, err
		}
		counts[i] = pc.PageCount
	}
	dst, err := inst.FPDF_CreateNewDocument(&requests.FPDF_CreateNewDocument{})
	if err != nil {
		return nil, err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: dst.Document}) //nolint:errcheck

	for i, ref := range pages {
		if ref.Source < 0 || ref.Source >= len(srcs) || ref.Page < 1 || ref.Page > counts[ref.Source] {
			return nil, fmt.Errorf("page %d of source %d doesn't exist", ref.Page, ref.Source)
		}
		rot, err := rotation(ref.Rotate)
		if err != nil {
			return nil, err
		}
		if _, err := inst.FPDF_ImportPagesByIndex(&requests.FPDF_ImportPagesByIndex{
			Source: docs[ref.Source], Destination: dst.Document, PageIndices: []int{ref.Page - 1}, Index: i}); err != nil {
			return nil, fmt.Errorf("import page: %w", err)
		}
		if rot != enums.FPDF_PAGE_ROTATION_NONE {
			page := requests.Page{ByIndex: &requests.PageByIndex{Document: dst.Document, Index: i}}
			cur, err := inst.FPDFPage_GetRotation(&requests.FPDFPage_GetRotation{Page: page})
			if err != nil {
				return nil, err
			}
			if _, err := inst.FPDFPage_SetRotation(&requests.FPDFPage_SetRotation{Page: page,
				Rotate: enums.FPDF_PAGE_ROTATION((int(cur.PageRotation) + int(rot)) % 4)}); err != nil {
				return nil, err
			}
		}
	}
	var buf bytes.Buffer
	if _, err := inst.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{Document: dst.Document, Flags: requests.SaveFlagNoIncremental, FileWriter: &buf}); err != nil {
		return nil, fmt.Errorf("save pdf: %w", err)
	}
	return buf.Bytes(), nil
}
