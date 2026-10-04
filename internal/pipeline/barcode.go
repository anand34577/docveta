package pipeline

import (
	"context"
	"image"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/platform/db"

	"github.com/klippa-app/go-pdfium/requests"
	"golang.org/x/image/draw"

	"github.com/anand34577/docveta/internal/barcode"
)

const (
	barcodeMaxPages = 500  // batches longer than this aren't scanned
	barcodeWidth    = 1500 // pixels: enough for a label's bars to be resolved
)

// ScanBarcodes renders the pages of a PDF and returns the barcode texts found on each page
// (1-based). It is only run for spaces that turned on batch splitting or ASN labels.
func (p *PDF) ScanBarcodes(s PDFSource) (map[int][]string, int, error) {
	inst, err := p.pool.GetInstance(10 * time.Minute)
	if err != nil {
		return nil, 0, err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(openReq(s))
	if err != nil {
		return nil, 0, mapOpenErr(err)
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck
	pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, 0, err
	}
	out := map[int][]string{}
	for i := 0; i < min(pc.PageCount, barcodeMaxPages); i++ {
		r, err := inst.RenderPageInPixels(&requests.RenderPageInPixels{
			Page:  requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}},
			Width: barcodeWidth, Height: barcodeWidth * 2,
		})
		if err != nil {
			continue
		}
		src := r.Result.RenderedImage
		img := image.NewRGBA(src.Bounds())
		draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
		r.Cleanup()
		if found := barcode.Decode(flattenWhite(img)); len(found) > 0 {
			out[i+1] = found
		}
	}
	return out, pc.PageCount, nil
}

// batchBarcodes looks for separator sheets and archive-number labels in a PDF when the
// space asked for it. With separators it queues the split and reports handled.
func (s *Service) batchBarcodes(ctx context.Context, d *docRow, src io.ReadSeeker, size int64) (separators []int, handled bool) {
	var split, readASN bool
	if err := s.pool.QueryRow(ctx, `SELECT split_on_separators, read_asn_barcodes FROM spaces WHERE id=$1`, d.SpaceID).Scan(&split, &readASN); err != nil || (!split && !readASN) {
		return nil, false
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	found, pages, err := s.pdf.ScanBarcodes(PDFSource{R: src, Size: size})
	_, _ = src.Seek(0, io.SeekStart)
	if err != nil {
		s.log.Warn("barcode scan failed", "document", d.ID, "err", err)
		return nil, false
	}
	asn := map[string]int64{}
	for page, texts := range found {
		for _, t := range texts {
			if split && barcode.IsSeparator(t) {
				separators = append(separators, page)
			}
			if n, ok := barcode.ASN(t); ok && readASN {
				if _, taken := asn[strconv.Itoa(page)]; !taken {
					asn[strconv.Itoa(page)] = n
				}
			}
		}
	}
	slices.Sort(separators)
	separators = slices.Compact(separators)
	// Worth splitting only when at least one real page remains.
	if len(separators) > 0 && len(separators) < pages {
		if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.setStatus(ctx, tx, d.ID, "processing", "splitting", ""); err != nil {
				return err
			}
			return s.queue.InsertTx(ctx, tx, jobs.SplitArgs{DocumentID: d.ID, Version: d.CurrentVersion, Separators: separators, ASN: asn},
				&river.InsertOpts{Priority: jobs.PriorityNormal, MaxAttempts: 3})
		}); err != nil {
			s.log.Warn("couldn't queue the batch split", "document", d.ID, "err", err)
			return separators, false
		}
		return separators, true
	}
	// Not a batch: a label still gives this document its archive number.
	if len(asn) > 0 {
		var first int64
		firstPage := 1 << 30
		for k, v := range asn {
			if pg, _ := strconv.Atoi(k); pg < firstPage {
				first, firstPage = v, pg
			}
		}
		_, _ = s.pool.Exec(ctx, `UPDATE documents SET asn=$2 WHERE id=$1 AND asn IS NULL AND NOT EXISTS (SELECT 1 FROM documents WHERE asn=$2)`, d.ID, first)
	}
	return separators, false
}
