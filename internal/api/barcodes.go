package api

import (
	"image/png"
	"net/http"
	"strconv"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/barcode"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

// registerBarcodes serves printable sheets: a separator page and archive-number labels.
func (a *API) registerBarcodes(mux router) {
	mux.HandleFunc("GET /api/v1/barcodes/separator.png", func(w http.ResponseWriter, r *http.Request) {
		if p := user(w, r); p == nil {
			return
		}
		img, err := barcode.SeparatorSheet()
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `inline; filename="docveta-separator.png"`)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		_ = png.Encode(w, img)
	})
	mux.HandleFunc("GET /api/v1/barcodes/asn.png", func(w http.ResponseWriter, r *http.Request) {
		if p := user(w, r); p == nil {
			return
		}
		n, err := strconv.ParseInt(r.URL.Query().Get("n"), 10, 64)
		if err != nil || n < 1 || n > 999999999999 {
			httpx.Error(w, r, apperr.Invalid("n", "Give an archive number, for example 42"))
			return
		}
		img, err := barcode.ASNLabel(n)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		_ = png.Encode(w, img)
	})
}
