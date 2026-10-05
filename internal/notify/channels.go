// Package notify delivers domain events to users: in-app (with live updates over
// Server-Sent Events) and external channels — Gotify, email (SMTP), ntfy and signed
// webhooks.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/netip"
	"net/smtp"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/jobs"
)

// Message is what a channel sends.
type Message struct {
	Title    string
	Body     string
	URL      string // absolute link into Docveta
	Severity string
	Event    jobs.Event
}

// GotifyConfig: create an "application" in Gotify and paste its token.
type GotifyConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type NtfyConfig struct {
	Server string `json:"server"` // default https://ntfy.sh
	Topic  string `json:"topic"`
	Token  string `json:"token,omitempty"`
}

type WebhookConfig struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// AppriseConfig sends through an Apprise API server (https://github.com/caronc/apprise-api),
// which fans out to 100+ services. Use Key for a saved configuration, or URLs for
// stateless delivery to one or more Apprise service URLs.
type AppriseConfig struct {
	URL  string `json:"url"`
	Key  string `json:"key,omitempty"`
	URLs string `json:"urls,omitempty"` // comma-separated Apprise URLs (stateless mode)
	Tag  string `json:"tag,omitempty"`
}

type EmailConfig struct {
	To string `json:"to"` // empty = the channel owner's account email
}

var (
	strictClient = newHTTPClient(false) // refuses local network addresses (SSRF protection)
	localClient  = newHTTPClient(true)
)

type allowLocalKey struct{}

// WithLocalTargets marks deliveries made with ctx as allowed to reach the local network:
// channels set up by administrators (a Gotify or ntfy server at home is the usual case).
func WithLocalTargets(ctx context.Context, allow bool) context.Context {
	return context.WithValue(ctx, allowLocalKey{}, allow)
}

func clientFor(ctx context.Context) *http.Client {
	if allow, _ := ctx.Value(allowLocalKey{}).(bool); allow {
		return localClient
	}
	return strictClient
}

// newHTTPClient returns the client used for user-defined URLs (Gotify, ntfy,
// webhooks). Unless allowLocal is set, it refuses to connect to loopback, private,
// link-local and CGNAT addresses. The check runs on the resolved IP at dial time, so
// it also covers DNS rebinding and redirects.
func newHTTPClient(allowLocal bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second}
	t := http.DefaultTransport.(*http.Transport).Clone()
	if !allowLocal {
		t.Proxy = nil // a proxy would hide the real destination from the check
		d.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if blockedAddr(ap.Addr()) {
				return fmt.Errorf("%s is a local network address; an administrator can allow these in Admin → System (Local network)", ap.Addr())
			}
			return nil
		}
	}
	t.DialContext = d.DialContext
	return &http.Client{Timeout: 15 * time.Second, Transport: t}
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func blockedAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() || cgnat.Contains(a)
}

func priorityFor(sev string) int {
	switch sev {
	case "error":
		return 8
	case "warning":
		return 6
	case "success":
		return 4
	}
	return 3
}

func sendGotify(ctx context.Context, c GotifyConfig, m Message) error {
	body, _ := json.Marshal(map[string]any{
		"title":    m.Title,
		"message":  m.Body,
		"priority": priorityFor(m.Severity),
		"extras": map[string]any{
			"client::notification": map[string]any{"click": map[string]string{"url": m.URL}},
			"client::display":      map[string]string{"contentType": "text/plain"},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/message", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", c.Token)
	return do(req)
}

func sendNtfy(ctx context.Context, c NtfyConfig, m Message) error {
	server := c.Server
	if server == "" {
		server = "https://ntfy.sh"
	}
	// JSON publishing: titles in Hindi or other scripts don't fit in HTTP headers.
	payload := map[string]any{"topic": c.Topic, "title": m.Title, "message": m.Body,
		"priority": min(5, max(1, priorityFor(m.Severity)/2+1)), "tags": []string{"page_facing_up"}}
	if m.URL != "" {
		payload["click"] = m.URL
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(server, "/")+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return do(req)
}

func appriseType(sev string) string {
	switch sev {
	case "error":
		return "failure"
	case "warning":
		return "warning"
	case "success":
		return "success"
	}
	return "info"
}

func sendApprise(ctx context.Context, c AppriseConfig, m Message) error {
	payload := map[string]any{"title": m.Title, "body": m.Body, "type": appriseType(m.Severity)}
	if m.URL != "" {
		payload["body"] = m.Body + "\n" + m.URL
	}
	if c.Tag != "" {
		payload["tag"] = c.Tag
	}
	endpoint := strings.TrimRight(c.URL, "/") + "/notify"
	if c.Key != "" {
		endpoint += "/" + url.PathEscape(c.Key)
	} else {
		payload["urls"] = c.URLs
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(req)
}

// sendWebhook posts the event as JSON with an HMAC-SHA256 signature header:
// Docveta-Signature: t=<unix>,v1=<hex(hmac(secret, "<t>.<body>"))>
func sendWebhook(ctx context.Context, c WebhookConfig, m Message) error {
	body, _ := json.Marshal(map[string]any{
		"id":    uuid.NewString(),
		"type":  m.Event.Type,
		"title": m.Title,
		"body":  m.Body,
		"url":   m.URL,
		"event": m.Event,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(c.Secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Docveta-Webhook/1")
	req.Header.Set("Docveta-Event", m.Event.Type)
	req.Header.Set("Docveta-Signature", "t="+ts+",v1="+hex.EncodeToString(mac.Sum(nil)))
	return do(req)
}

// PostJSON posts a JSON document to a user-defined address (workflow webhooks). Like
// notification channels it refuses local network addresses unless an administrator allows them.
func PostJSON(ctx context.Context, rawURL string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Docveta-Workflow/1")
	return do(req)
}

func do(req *http.Request) error {
	resp, err := clientFor(req.Context()).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// SMTP
// ---------------------------------------------------------------------------

type SMTPConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // starttls | tls | none
	Username string `json:"username"`
	From     string `json:"from"`
	// Password is write-only.
	Password    *string `json:"password,omitempty"`
	HasPassword bool    `json:"has_password"`
}

func sendEmail(ctx context.Context, c SMTPConfig, password, to string, m Message) error {
	if !c.Enabled || c.Host == "" {
		return errors.New("email (SMTP) is not configured")
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if c.Security == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if c.Security == "starttls" {
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if c.Username != "" {
		if err := cl.Auth(smtpAuth(cl, c, password)); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	from := c.From
	if from == "" {
		from = c.Username
	}
	if err := cl.Mail(addrOnly(from)); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildEmail(from, to, m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// smtpAuth picks a login method the server offers: PLAIN when it's advertised (or nothing is),
// otherwise LOGIN, which Microsoft 365 / Outlook.com and some hosting providers require.
func smtpAuth(cl *smtp.Client, c SMTPConfig, password string) smtp.Auth {
	_, mechs := cl.Extension("AUTH")
	offered := strings.Fields(strings.ToUpper(mechs))
	if len(offered) > 0 && !slices.Contains(offered, "PLAIN") && slices.Contains(offered, "LOGIN") {
		return loginAuth{c.Username, password}
	}
	if c.Security == "none" {
		return plainAuth{c.Username, password} // the admin chose "None": a relay or proxy on their own network
	}
	return smtp.PlainAuth("", c.Username, password, c.Host)
}

// loginAuth is the AUTH LOGIN mechanism (username, then password, each base64 by net/smtp).
type loginAuth struct{ user, pass string }

func (a loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }

func (a loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(challenge))) {
	case "username:", "user name", "username":
		return []byte(a.user), nil
	case "password:", "password":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected server challenge %q", challenge)
}

// plainAuth is smtp.PlainAuth without its refusal to send the password over a connection that
// isn't encrypted ("unencrypted connection"), for servers the admin set to Security "None".
type plainAuth struct{ user, pass string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

func addrOnly(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		return strings.TrimSuffix(s[i+1:], ">")
	}
	return s
}

// encodeHeader RFC 2047-encodes non-ASCII header values.
func encodeHeader(s string) string { return mime.BEncoding.Encode("UTF-8", s) }

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func buildEmail(from, to string, m Message) []byte {
	boundary := "dvt-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nMessage-ID: <%s@docveta>\r\n",
		encodeHeader(from), to, encodeHeader(m.Title), time.Now().Format(time.RFC1123Z), uuid.NewString())
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	text := m.Body
	if m.URL != "" {
		text += "\n\nOpen in Docveta: " + m.URL
	}
	html := `<div style="font-family:system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:560px;margin:auto;padding:24px;color:#1f2430">` +
		`<div style="font-size:13px;color:#6b7280;margin-bottom:16px">Docveta</div>` +
		`<h2 style="font-size:18px;margin:0 0 12px">` + htmlEscape(m.Title) + `</h2>` +
		`<p style="font-size:15px;line-height:1.5;margin:0 0 20px;white-space:pre-line">` + htmlEscape(m.Body) + `</p>`
	if m.URL != "" {
		html += `<a href="` + htmlEscape(m.URL) + `" style="display:inline-block;background:#3b4fd8;color:#fff;text-decoration:none;padding:10px 16px;border-radius:8px;font-size:14px">Open in Docveta</a>`
	}
	html += `<p style="font-size:12px;color:#9ca3af;margin-top:28px">You can change which emails you get in Docveta → Settings → Notifications.</p></div>`

	for _, part := range []struct{ ct, body string }{{"text/plain", text}, {"text/html", html}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.ct)
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(part.body))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes()
}
