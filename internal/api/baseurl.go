package api

import (
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/anand34577/docveta/internal/platform/httpx"
)

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)

func isLoopbackHost(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// publicBase is the address the person is using, without a trailing slash, for links and the
// single sign-on redirect. DOCVETA_BASE_URL wins when it names a real host. The installers set
// it to http://localhost:<port>, so for anyone reaching Docveta another way (a LAN address, a
// reverse proxy) the request's own address is used; otherwise SSO would send the provider a
// localhost redirect URI that it rejects, and shared links would point at localhost.
func (a *API) publicBase(r *http.Request) string {
	b := a.Cfg.BaseURL
	base := strings.TrimRight(b.String(), "/")
	if !isLoopbackHost(b.Hostname()) {
		return base
	}
	host, scheme := r.Host, "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if httpx.FromTrustedProxy(r, a.Cfg.TrustedProxies) {
		if h := firstValue(r.Header.Get("X-Forwarded-Host")); h != "" {
			host = h
		}
		if p := strings.ToLower(firstValue(r.Header.Get("X-Forwarded-Proto"))); p == "https" || p == "http" {
			scheme = p
		}
	}
	if !hostRe.MatchString(host) {
		return base
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		if isLoopbackHost(h) {
			return base
		}
	} else if isLoopbackHost(host) {
		return base
	}
	return scheme + "://" + host + strings.TrimRight(b.Path, "/")
}

func firstValue(h string) string {
	v, _, _ := strings.Cut(h, ",")
	return strings.TrimSpace(v)
}

// originOf returns scheme://host of a base URL.
func originOf(base string) string {
	if i := strings.Index(base, "://"); i >= 0 {
		if j := strings.IndexByte(base[i+3:], '/'); j >= 0 {
			return base[:i+3+j]
		}
	}
	return base
}

// rememberAddress keeps the address people use when DOCVETA_BASE_URL is only a localhost
// default, so links in emails and push notifications (sent later, without a request) work.
func (a *API) rememberAddress(r *http.Request) {
	if a.Settings == nil || !isLoopbackHost(a.Cfg.BaseURL.Hostname()) {
		return
	}
	if b := a.publicBase(r); b != strings.TrimRight(a.Cfg.BaseURL.String(), "/") {
		a.Settings.RememberPublicURL(r.Context(), b)
	}
}
