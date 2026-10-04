// Package office converts Word, Excel, PowerPoint and OpenDocument files to PDF with a
// Gotenberg server (https://gotenberg.dev), so they can be previewed and searched like
// any other document. The original is always kept.
package office

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anand34577/docveta/internal/apperr"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/settings"
)

// Settings is the admin-editable part of the configuration.
type Settings struct {
	URL string `json:"url"` // overrides DOCVETA_GOTENBERG_URL when set
}

// Info is what Administration shows.
type Info struct {
	URL     string `json:"url"`      // the address in use
	FromEnv bool   `json:"from_env"` // true when it comes from DOCVETA_GOTENBERG_URL
	Enabled bool   `json:"enabled"`
}

type Converter struct {
	settings *settings.Store
	envURL   string
	http     *http.Client
}

func New(st *settings.Store, envURL string) *Converter {
	// The address is chosen by an administrator, so local network targets are fine here.
	return &Converter{settings: st, envURL: strings.TrimRight(envURL, "/"), http: &http.Client{Timeout: 5 * time.Minute}}
}

const key = "office"

func (c *Converter) saved(ctx context.Context) string {
	var s Settings
	_, _ = c.settings.Get(ctx, key, &s)
	return strings.TrimRight(s.URL, "/")
}

// URL returns the Gotenberg address in use ("" = not configured).
func (c *Converter) URL(ctx context.Context) string {
	if u := c.saved(ctx); u != "" {
		return u
	}
	return c.envURL
}

func (c *Converter) Enabled(ctx context.Context) bool { return c.URL(ctx) != "" }

func (c *Converter) Info(ctx context.Context, p *auth.Principal) (*Info, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	u := c.URL(ctx)
	return &Info{URL: u, FromEnv: c.saved(ctx) == "" && c.envURL != "", Enabled: u != ""}, nil
}

func valid(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Set saves the address; "" falls back to the environment setting.
func (c *Converter) Set(ctx context.Context, p *auth.Principal, raw string) (*Info, error) {
	if !p.Admin() {
		return nil, apperr.Forbidden("")
	}
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw != "" && !valid(raw) {
		return nil, apperr.Invalid("url", "Enter the converter's address, e.g. http://gotenberg:3000")
	}
	if err := c.settings.Set(ctx, key, Settings{URL: raw}, p.UserID); err != nil {
		return nil, err
	}
	return c.Info(ctx, p)
}

// Test checks the server answers its health endpoint.
func (c *Converter) Test(ctx context.Context, p *auth.Principal, raw string) error {
	if !p.Admin() {
		return apperr.Forbidden("")
	}
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		raw = c.URL(ctx)
	}
	if !valid(raw) {
		return apperr.Invalid("url", "Enter the converter's address")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw+"/health", nil)
	res, err := c.http.Do(req)
	if err != nil {
		return &apperr.Error{Kind: apperr.KindValidation, Code: "unreachable", Msg: "Can't reach the converter: " + err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return &apperr.Error{Kind: apperr.KindValidation, Code: "unhealthy", Msg: fmt.Sprintf("The converter answered %d. Is this a Gotenberg server?", res.StatusCode)}
	}
	return nil
}

// Convert turns a document into PDF. ext (".docx", ".xlsx", ...) tells LibreOffice what it is.
func (c *Converter) Convert(ctx context.Context, r io.Reader, ext string) ([]byte, error) {
	base := c.URL(ctx)
	if base == "" {
		return nil, fmt.Errorf("document conversion isn't set up")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "document"+ext)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, r); err != nil {
		return nil, err
	}
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/forms/libreoffice/convert", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("converter unreachable: %w", err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 512<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("converter answered %d: %s", res.StatusCode, msg)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		return nil, fmt.Errorf("converter didn't return a PDF")
	}
	return out, nil
}
