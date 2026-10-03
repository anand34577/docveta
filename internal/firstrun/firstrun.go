// Package firstrun serves the database setup page shown when Docveta starts without a
// database configured. The person installing Docveta either picks the built-in database
// (Docveta runs its own PostgreSQL in the data folder; nothing to install or type) or
// enters the connection details of their PostgreSQL server, which Docveta tests and can
// create the database on. The choice is saved to the data folder's docveta.conf and
// Docveta then starts normally.
//
// Requests from this computer are accepted directly. Requests from other computers
// must include the one-time setup code that Docveta prints in its log and writes to
// setup-code.txt in the data folder, so nobody else on the network can take over a
// fresh install.
package firstrun

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anand34577/docveta/internal/platform/builtindb"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

//go:embed setup.html
var pageFS embed.FS

var page = template.Must(template.ParseFS(pageFS, "setup.html"))

// Form holds what the user typed (re-rendered on errors).
type Form struct {
	Kind                                                string // "builtin" or "server"
	Host, Port, User, Password, Database, SSLMode, Code string
	Create                                              bool
}

type view struct {
	Form
	NeedCode bool
	Builtin  bool // the built-in database is available on this system
	Error    string
	OK       string
	Done     bool
	CodeFile string
}

// Run serves the setup page on cfg.ListenAddr until working database settings are
// saved, and returns the database URL (builtindb.Setting for the built-in database).
// It returns ctx.Err() if Docveta is stopped first.
func Run(ctx context.Context, cfg *config.Config, log *slog.Logger) (string, error) {
	code := newCode()
	codeFile := filepath.Join(cfg.DataDir, "setup-code.txt")
	if err := os.WriteFile(codeFile, []byte(code+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", codeFile, err)
	}
	defer os.Remove(codeFile)

	done := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("setup")) })
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if !sameOrigin(r) {
			http.Error(w, "cross-site request blocked", http.StatusForbidden)
			return
		}
		f := Form{
			Kind: r.FormValue("kind"),
			Host: strings.TrimSpace(r.FormValue("host")), Port: strings.TrimSpace(r.FormValue("port")),
			User: strings.TrimSpace(r.FormValue("user")), Password: r.FormValue("password"),
			Database: strings.TrimSpace(r.FormValue("database")), SSLMode: r.FormValue("sslmode"),
			Code: strings.TrimSpace(r.FormValue("code")), Create: r.FormValue("create") == "on",
		}
		v := view{Form: f, NeedCode: !isLocal(r), Builtin: builtindb.Supported(), CodeFile: codeFile}
		if v.NeedCode && subtle.ConstantTimeCompare([]byte(strings.ToUpper(f.Code)), []byte(code)) != 1 {
			time.Sleep(time.Second) // slows down guessing
			v.Error = "The setup code is wrong. It is printed in the Docveta log and saved in " + codeFile + "."
			render(w, v)
			return
		}
		if f.Kind == "builtin" && v.Builtin {
			// Download and create it now, so problems show here rather than after saving.
			if err := builtindb.Prepare(r.Context(), cfg.DataDir, log); err != nil {
				v.Error = "Couldn't set up the built-in database: " + err.Error()
				render(w, v)
				return
			}
			saveAndFinish(w, v, cfg, log, builtindb.Setting, done)
			return
		}
		v.Kind = "server"
		dbURL, err := f.URL()
		if err == nil {
			cctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			var version string
			version, err = Check(cctx, dbURL, f.Create)
			cancel()
			if err == nil {
				v.OK = "Connected to PostgreSQL " + version + "."
			}
		}
		if err != nil {
			v.Error = err.Error()
			render(w, v)
			return
		}
		if r.FormValue("action") != "save" {
			v.OK += " Everything Docveta needs is in place. Click Save and start."
			render(w, v)
			return
		}
		saveAndFinish(w, v, cfg, log, dbURL, done)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" { // the web app and API aren't available yet
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		kind := "server"
		if builtindb.Supported() {
			kind = "builtin"
		}
		render(w, view{Form: Form{Kind: kind, Host: "localhost", Port: "5432", User: "docveta", Database: "docveta", SSLMode: "prefer", Create: true},
			NeedCode: !isLocal(r), Builtin: builtindb.Supported(), CodeFile: codeFile})
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpx.SecurityHeaders(cfg.BaseURL.Scheme == "https")(mux),
		ReadHeaderTimeout: 15 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Warn("Docveta needs a database. Open the setup page in your browser to connect it to PostgreSQL.",
		"url", cfg.BaseURL.String(), "setup_code_for_other_computers", code)

	var dbURL string
	select {
	case err := <-errc:
		return "", fmt.Errorf("setup page: %w", err)
	case <-ctx.Done():
	case dbURL = <-done:
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx) // lets the "starting…" page finish sending
	if dbURL == "" {
		return "", ctx.Err()
	}
	return dbURL, nil
}

// saveAndFinish stores the database setting and hands it to Run.
func saveAndFinish(w http.ResponseWriter, v view, cfg *config.Config, log *slog.Logger, dbURL string, done chan<- string) {
	if err := config.Save(cfg.DataDir, map[string]string{"DOCVETA_DATABASE_URL": dbURL}); err != nil {
		v.Error = "Couldn't save the settings: " + err.Error()
		render(w, v)
		return
	}
	log.Info("database configured; starting Docveta", "config", config.DataFile(cfg.DataDir))
	v.Done = true
	render(w, v)
	select {
	case done <- dbURL:
	default: // already saved by an earlier click
	}
}

func render(w http.ResponseWriter, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = page.Execute(w, v)
}

// URL builds a postgres:// URL from the form.
func (f Form) URL() (string, error) {
	if f.Host == "" || f.User == "" || f.Database == "" {
		return "", errors.New("Server, user and database name are required.")
	}
	port, err := strconv.Atoi(f.Port)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("Port must be a number between 1 and 65535 (PostgreSQL uses 5432 by default).")
	}
	switch f.SSLMode {
	case "disable", "prefer", "require", "verify-full":
	default:
		f.SSLMode = "prefer"
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(f.User, f.Password), Host: net.JoinHostPort(f.Host, strconv.Itoa(port)),
		Path: "/" + f.Database, RawQuery: "sslmode=" + f.SSLMode}
	return u.String(), nil
}

// Check connects to the database (creating it first if asked and it doesn't exist)
// and verifies everything Docveta needs, returning the server version. Errors are
// written for the person filling in the form.
func Check(ctx context.Context, dbURL string, create bool) (string, error) {
	conn, err := pgx.Connect(ctx, dbURL)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "3D000" && create { // database doesn't exist
		if err := createDatabase(ctx, dbURL); err != nil {
			return "", err
		}
		conn, err = pgx.Connect(ctx, dbURL)
	}
	if err != nil {
		return "", explain(err)
	}
	defer conn.Close(context.Background())

	var num int
	var version string
	if err := conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int, current_setting('server_version')`).Scan(&num, &version); err != nil {
		return "", explain(err)
	}
	if num < 160000 {
		return "", fmt.Errorf("This server runs PostgreSQL %s; Docveta needs version 16 or newer.", version)
	}
	var missing []string
	for _, ext := range []string{"pg_trgm", "citext"} {
		var ok bool
		_ = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name=$1)`, ext).Scan(&ok)
		if !ok {
			missing = append(missing, ext)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("The PostgreSQL extensions %s aren't installed on the server. Install the PostgreSQL \"contrib\" package (on Debian/Ubuntu: postgresql-contrib).", strings.Join(missing, " and "))
	}
	// Try what the first start will do, then undo it.
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", explain(err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck
	for _, stmt := range []string{"CREATE EXTENSION IF NOT EXISTS pg_trgm", "CREATE EXTENSION IF NOT EXISTS citext", "CREATE TABLE docveta_setup_check (x int)"} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return "", fmt.Errorf("The user can connect but isn't allowed to set up the database (%s). Make the user the owner of the database: ALTER DATABASE <name> OWNER TO <user>;", firstLine(err))
		}
	}
	return version, nil
}

func createDatabase(ctx context.Context, dbURL string) error {
	u, _ := url.Parse(dbURL)
	name := strings.TrimPrefix(u.Path, "/")
	u.Path = "/postgres"
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return fmt.Errorf("The database %q doesn't exist, and Docveta couldn't connect to create it: %v", name, explain(err))
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" ENCODING 'UTF8' TEMPLATE template0"); err != nil {
		return fmt.Errorf("The database %q doesn't exist and this user isn't allowed to create it (%s). Create it as an administrator, or enter an administrator account such as \"postgres\".", name, firstLine(err))
	}
	return nil
}

// explain turns connection errors into advice.
func explain(err error) error {
	var pgErr *pgconn.PgError
	msg := strings.Join(strings.Fields(err.Error()), " ") // pgx puts the cause on later lines
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "28P01":
		return errors.New("The user name or password is wrong.")
	case errors.As(err, &pgErr) && pgErr.Code == "3D000":
		return errors.New("That database doesn't exist. Tick \"Create the database if it doesn't exist\", or create it yourself.")
	case errors.As(err, &pgErr) && pgErr.Code == "28000":
		return fmt.Errorf("The server refused this connection: %s. Allow it in the server's pg_hba.conf.", pgErr.Message)
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused"):
		return errors.New("Nothing is answering at that server and port. Is PostgreSQL installed and running?")
	case strings.Contains(msg, "no such host"):
		return errors.New("That server name can't be found. Check the spelling, or use its IP address.")
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return errors.New("The server didn't answer in time. Check the address and any firewall in between.")
	case strings.Contains(msg, "server refused TLS") || strings.Contains(msg, "SSL is not enabled"):
		return errors.New("The server doesn't support encrypted connections. Choose \"Off\" or \"Use if available\" under Encryption.")
	}
	return errors.New(msg)
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}

// isLocal reports whether the request comes straight from this computer (not via a
// proxy, which would make every visitor look local).
func isLocal(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" || r.Header.Get("X-Real-Ip") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameOrigin blocks other websites from submitting the form through the user's browser.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true // not a browser
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// newCode returns a short code that is easy to read and type (no 0/O, 1/I).
func newCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:5]) + "-" + string(b[5:])
}
