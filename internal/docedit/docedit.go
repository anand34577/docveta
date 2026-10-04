// Package docedit changes the file of an existing document: unlock a password-protected
// PDF, rotate/delete/reorder pages, split a document in parts, merge several into one
// and put in a new file. Every change adds a version, so nothing is ever lost.
package docedit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/documents"
	"github.com/anand34577/docveta/internal/jobs"
	"github.com/anand34577/docveta/internal/pipeline"
	"github.com/anand34577/docveta/internal/spaces"
)

type Service struct {
	docs *documents.Service
	pdf  *pipeline.PDF
}

func NewService(docs *documents.Service, pdf *pipeline.PDF) *Service {
	return &Service{docs: docs, pdf: pdf}
}

// maxEditBytes bounds the PDFs we load into the PDF engine's memory.
const maxEditBytes = 300 << 20

func (s *Service) open(ctx context.Context, p *auth.Principal, id uuid.UUID) (*documents.File, *bytes.Reader, error) {
	f, err := s.docs.OpenFile(ctx, p, id, "original")
	if err != nil {
		return nil, nil, err
	}
	defer f.Reader.Close()
	if f.Size > maxEditBytes {
		return nil, nil, apperr.Invalid("file", "This file is too large to edit pages")
	}
	b, err := io.ReadAll(io.LimitReader(f.Reader, maxEditBytes+1))
	if err != nil {
		return nil, nil, err
	}
	return f, bytes.NewReader(b), nil
}

func requirePDF(a *documents.Access, what string) error {
	if a.Mime != documents.MimePDF {
		return &apperr.Error{Kind: apperr.KindUnsupported, Code: "not_a_pdf", Msg: what + " works on PDF documents"}
	}
	if a.Deleted {
		return apperr.Conflict("in_trash", "Restore this document from Trash first")
	}
	return nil
}

// Unlock removes the password from an encrypted PDF. The original stays as an earlier
// version; the unlocked copy becomes current and is processed (text, search). The
// password is used in memory only.
func (s *Service) Unlock(ctx context.Context, p *auth.Principal, id uuid.UUID, password string) (*documents.Document, error) {
	a, err := s.docs.Access(ctx, p, id, spaces.ActEdit)
	if err != nil {
		return nil, err
	}
	if err := requirePDF(a, "Unlocking"); err != nil {
		return nil, err
	}
	if a.Status != "needs_password" {
		return nil, apperr.Conflict("not_locked", "This document isn't password protected")
	}
	if password == "" {
		return nil, apperr.Invalid("password", "Enter the password")
	}
	_, r, err := s.open(ctx, p, id)
	if err != nil {
		return nil, err
	}
	out, err := s.pdf.Unlock(pipeline.PDFSource{R: r, Size: r.Size(), Password: password})
	if errors.Is(err, pipeline.ErrPassword) {
		return nil, apperr.Invalid("password", "That password isn't right")
	}
	if err != nil {
		return nil, &apperr.Error{Kind: apperr.KindValidation, Code: "unlock_failed", Msg: "This PDF couldn't be unlocked", Err: err}
	}
	return s.docs.AddVersionBytes(ctx, p, id, out, documents.MimePDF, "Unlocked (password removed)")
}

// PageOp is one page of the edited document: which original page, and how much to turn it.
type PageOp struct {
	From   int `json:"from"`   // 1-based page of the current file
	Rotate int `json:"rotate"` // degrees clockwise to add (0, 90, 180, 270)
}

// EditPages rebuilds the file from the listed pages, in the listed order. Omitted
// pages are deleted. Single-page JPEG/PNG images can be rotated too.
func (s *Service) EditPages(ctx context.Context, p *auth.Principal, id uuid.UUID, pages []PageOp) (*documents.Document, error) {
	a, err := s.docs.Access(ctx, p, id, spaces.ActEdit)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, apperr.Invalid("pages", "A document needs at least one page")
	}
	if a.Mime == documents.MimeJPEG || a.Mime == documents.MimePNG {
		return s.rotateImage(ctx, p, a, pages)
	}
	if err := requirePDF(a, "Page editing"); err != nil {
		return nil, err
	}
	if a.Status == "needs_password" {
		return nil, apperr.Conflict("locked", "Unlock this PDF first")
	}
	_, r, err := s.open(ctx, p, id)
	if err != nil {
		return nil, err
	}
	refs := make([]pipeline.PageRef, len(pages))
	changed := false
	for i, op := range pages {
		if op.Rotate%90 != 0 {
			return nil, apperr.Invalid("pages", "Rotate by 90, 180 or 270 degrees")
		}
		refs[i] = pipeline.PageRef{Source: 0, Page: op.From, Rotate: op.Rotate}
		changed = changed || op.Rotate%360 != 0 || op.From != i+1
	}
	n, err := s.pdf.PageCountOf(pipeline.PDFSource{R: r, Size: r.Size()})
	if err != nil {
		return nil, err
	}
	changed = changed || len(pages) != n
	if !changed {
		return s.docs.Hydrated(ctx, id)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	out, err := s.pdf.Build([]pipeline.PDFSource{{R: r, Size: r.Size()}}, refs)
	if err != nil {
		return nil, &apperr.Error{Kind: apperr.KindValidation, Code: "edit_failed", Msg: "Those pages couldn't be rearranged: " + err.Error()}
	}
	return s.docs.AddVersionBytes(ctx, p, id, out, documents.MimePDF, describeEdit(pages, n))
}

func describeEdit(pages []PageOp, before int) string {
	rot := 0
	for _, p := range pages {
		if p.Rotate%360 != 0 {
			rot++
		}
	}
	var parts []string
	if len(pages) < before {
		parts = append(parts, fmt.Sprintf("deleted %d page(s)", before-len(pages)))
	}
	if rot > 0 {
		parts = append(parts, fmt.Sprintf("rotated %d page(s)", rot))
	}
	if len(parts) == 0 {
		parts = append(parts, "reordered pages")
	}
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

func (s *Service) rotateImage(ctx context.Context, p *auth.Principal, a *documents.Access, pages []PageOp) (*documents.Document, error) {
	if len(pages) != 1 || pages[0].From != 1 {
		return nil, apperr.Invalid("pages", "An image has a single page")
	}
	deg := ((pages[0].Rotate % 360) + 360) % 360
	if deg%90 != 0 {
		return nil, apperr.Invalid("pages", "Rotate by 90, 180 or 270 degrees")
	}
	if deg == 0 {
		return s.docs.Hydrated(ctx, a.ID)
	}
	f, r, err := s.open(ctx, p, a.ID)
	if err != nil {
		return nil, err
	}
	img, _, err := pipeline.DecodeImage(r)
	if err != nil {
		return nil, &apperr.Error{Kind: apperr.KindValidation, Code: "edit_failed", Msg: "This image couldn't be read", Err: err}
	}
	img = rotateClockwise(img, deg)
	var buf bytes.Buffer
	if f.Mime == documents.MimePNG {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	}
	if err != nil {
		return nil, err
	}
	return s.docs.AddVersionBytes(ctx, p, a.ID, buf.Bytes(), f.Mime, "Rotated "+strconv.Itoa(deg)+"°")
}

func rotateClockwise(src image.Image, deg int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var dst *image.NRGBA
	switch deg {
	case 90, 270:
		dst = image.NewNRGBA(image.Rect(0, 0, h, w))
	default:
		dst = image.NewNRGBA(image.Rect(0, 0, w, h))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			switch deg {
			case 90:
				dst.Set(h-1-y, x, c)
			case 180:
				dst.Set(w-1-x, h-1-y, c)
			case 270:
				dst.Set(y, w-1-x, c)
			}
		}
	}
	return dst
}

// ParseRanges turns "1-3, 5, 7-" into page numbers (1..n); "7-" runs to the last page.
func ParseRanges(spec string, n int) ([]int, error) {
	var out []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || a < 1 {
			return nil, fmt.Errorf("%q isn't a page number", part)
		}
		b := a
		if isRange {
			if strings.TrimSpace(hi) == "" {
				b = n
			} else if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				return nil, fmt.Errorf("%q isn't a page range", part)
			}
		}
		if b < a || b > n {
			return nil, fmt.Errorf("%q is outside the document's %d pages", part, n)
		}
		for i := a; i <= b; i++ {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no pages selected")
	}
	return out, nil
}

// Split creates one new document per page range, copying the original's metadata.
// With trashOriginal the source goes to the Trash afterwards.
func (s *Service) Split(ctx context.Context, p *auth.Principal, id uuid.UUID, ranges []string, trashOriginal bool) ([]*documents.Document, error) {
	a, err := s.docs.Access(ctx, p, id, spaces.ActEdit)
	if err != nil {
		return nil, err
	}
	if err := requirePDF(a, "Splitting"); err != nil {
		return nil, err
	}
	if a.Status == "needs_password" {
		return nil, apperr.Conflict("locked", "Unlock this PDF first")
	}
	if len(ranges) < 2 {
		return nil, apperr.Invalid("ranges", "Give at least two page ranges, for example 1-3 and 4-6")
	}
	src, err := s.docs.Hydrated(ctx, id)
	if err != nil {
		return nil, err
	}
	_, r, err := s.open(ctx, p, id)
	if err != nil {
		return nil, err
	}
	n, err := s.pdf.PageCountOf(pipeline.PDFSource{R: r, Size: r.Size()})
	if err != nil {
		return nil, err
	}
	// Validate everything before creating anything.
	parts := make([][]int, len(ranges))
	for i, spec := range ranges {
		if parts[i], err = ParseRanges(spec, n); err != nil {
			return nil, apperr.Invalid("ranges", err.Error())
		}
	}
	tagIDs := make([]uuid.UUID, len(src.Tags))
	for i, t := range src.Tags {
		tagIDs[i] = t.ID
	}
	var out []*documents.Document
	for i, pages := range parts {
		refs := make([]pipeline.PageRef, len(pages))
		for j, pg := range pages {
			refs[j] = pipeline.PageRef{Page: pg}
		}
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		b, err := s.pdf.Build([]pipeline.PDFSource{{R: r, Size: r.Size()}}, refs)
		if err != nil {
			return out, err
		}
		in := documents.IngestInput{SpaceID: a.SpaceID, Filename: src.OriginalFilename, Title: fmt.Sprintf("%s (part %d)", src.Title, i+1),
			TagIDs: tagIDs, DocumentDate: src.DocumentDate, Language: src.Language, Source: "web", AllowDuplicate: true}
		if src.Correspondent != nil {
			in.CorrespondentID = &src.Correspondent.ID
		}
		if src.DocumentType != nil {
			in.DocumentTypeID = &src.DocumentType.ID
		}
		d, err := s.docs.Ingest(ctx, p, in, bytes.NewReader(b))
		if err != nil {
			return out, err
		}
		out = append(out, d)
	}
	if trashOriginal {
		if err := s.docs.Trash(ctx, p, id); err != nil {
			return out, err
		}
	}
	return out, nil
}

// Merge joins PDFs (all pages, in the given order) into a new document in the first
// document's space. With trashOriginals the sources go to the Trash afterwards.
func (s *Service) Merge(ctx context.Context, p *auth.Principal, ids []uuid.UUID, title string, trashOriginals bool) (*documents.Document, error) {
	if len(ids) < 2 {
		return nil, apperr.Invalid("ids", "Choose at least two documents to merge")
	}
	if len(ids) > 50 {
		return nil, apperr.Invalid("ids", "Merge at most 50 documents at once")
	}
	act := spaces.ActView
	if trashOriginals {
		act = spaces.ActEdit
	}
	srcs := make([]pipeline.PDFSource, len(ids))
	var refs []pipeline.PageRef
	var first *documents.Access
	total := int64(0)
	for i, id := range ids {
		a, err := s.docs.Access(ctx, p, id, act)
		if err != nil {
			return nil, err
		}
		if err := requirePDF(a, "Merging"); err != nil {
			return nil, err
		}
		if a.Status == "needs_password" {
			return nil, apperr.Conflict("locked", "\""+a.Title+"\" is locked. Unlock it first.")
		}
		if i == 0 {
			first = a
		}
		_, r, err := s.open(ctx, p, id)
		if err != nil {
			return nil, err
		}
		if total += r.Size(); total > maxEditBytes {
			return nil, apperr.Invalid("ids", "These documents are too large to merge together")
		}
		n, err := s.pdf.PageCountOf(pipeline.PDFSource{R: r, Size: r.Size()})
		if err != nil {
			return nil, err
		}
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		srcs[i] = pipeline.PDFSource{R: r, Size: r.Size()}
		for pg := 1; pg <= n; pg++ {
			refs = append(refs, pipeline.PageRef{Source: i, Page: pg})
		}
	}
	b, err := s.pdf.Build(srcs, refs)
	if err != nil {
		return nil, &apperr.Error{Kind: apperr.KindValidation, Code: "merge_failed", Msg: "These documents couldn't be merged", Err: err}
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = first.Title + " (merged)"
	}
	d, err := s.docs.Ingest(ctx, p, documents.IngestInput{SpaceID: first.SpaceID, Filename: title + ".pdf", Title: title, Source: "web", AllowDuplicate: true}, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if trashOriginals {
		for _, id := range ids {
			if err := s.docs.Trash(ctx, p, id); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

// SplitWorker splits a scanned batch at its separator sheets (queued by preprocessing).
type SplitWorker struct {
	river.WorkerDefaults[jobs.SplitArgs]
	S *Service
}

func (w *SplitWorker) Timeout(*river.Job[jobs.SplitArgs]) time.Duration { return 30 * time.Minute }

func (w *SplitWorker) Work(ctx context.Context, job *river.Job[jobs.SplitArgs]) error {
	return w.S.SplitBatch(ctx, job.Args)
}

// SplitBatch turns a batch scan into one document per run of pages between separator
// sheets (the separators themselves are dropped). Each part keeps the batch's metadata,
// and a part whose pages carry an archive-number label gets that number. The batch goes
// to the Trash, where it can still be restored.
func (s *Service) SplitBatch(ctx context.Context, a jobs.SplitArgs) error {
	p := s.docs.Principal(ctx, a.DocumentID)
	access, err := s.docs.Access(ctx, auth.System(), a.DocumentID, spaces.ActView)
	if err != nil || access.Deleted || access.CurrentVersion != a.Version {
		return nil // gone, trashed or replaced meanwhile
	}
	src, err := s.docs.Hydrated(ctx, a.DocumentID)
	if err != nil {
		return err
	}
	_, r, err := s.open(ctx, auth.System(), a.DocumentID)
	if err != nil {
		return err
	}
	n, err := s.pdf.PageCountOf(pipeline.PDFSource{R: r, Size: r.Size()})
	if err != nil {
		return err
	}
	isSep := map[int]bool{}
	for _, pg := range a.Separators {
		isSep[pg] = true
	}
	var parts [][]int
	var cur []int
	for pg := 1; pg <= n; pg++ {
		if isSep[pg] {
			if len(cur) > 0 {
				parts = append(parts, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, pg)
	}
	if len(cur) > 0 {
		parts = append(parts, cur)
	}
	if len(parts) == 0 {
		return nil
	}
	tagIDs := make([]uuid.UUID, len(src.Tags))
	for i, t := range src.Tags {
		tagIDs[i] = t.ID
	}
	for i, pages := range parts {
		refs := make([]pipeline.PageRef, len(pages))
		for j, pg := range pages {
			refs[j] = pipeline.PageRef{Page: pg}
		}
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return err
		}
		b, err := s.pdf.Build([]pipeline.PDFSource{{R: r, Size: r.Size()}}, refs)
		if err != nil {
			return err
		}
		in := documents.IngestInput{SpaceID: access.SpaceID, Filename: src.OriginalFilename, Title: fmt.Sprintf("%s (%d of %d)", src.Title, i+1, len(parts)),
			TagIDs: tagIDs, DocumentDate: src.DocumentDate, Language: src.Language, Source: "scan", AllowDuplicate: true, Priority: jobs.PriorityNormal}
		if src.Correspondent != nil {
			in.CorrespondentID = &src.Correspondent.ID
		}
		if src.DocumentType != nil {
			in.DocumentTypeID = &src.DocumentType.ID
		}
		d, err := s.docs.Ingest(ctx, p, in, bytes.NewReader(b))
		if err != nil {
			return err
		}
		for _, pg := range pages {
			if asn, ok := a.ASN[strconv.Itoa(pg)]; ok {
				s.docs.SetASNIfFree(ctx, d.ID, asn)
				break
			}
		}
	}
	return s.docs.FinishSplit(ctx, a.DocumentID, len(parts))
}
