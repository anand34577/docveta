// Command docveta is the Docveta server and admin CLI.
//
//	docveta serve                      run the web server and background jobs (default);
//	                                 without a database it serves the database setup page
//	docveta migrate                    apply database migrations and exit
//	docveta user create --email E --name N [--admin]   (prompts for password via DOCVETA_PASSWORD)
//	docveta user reset-password --email E              (reads DOCVETA_PASSWORD)
//	docveta doctor [--verify-blobs]    check configuration, database, storage and workers
//	docveta service install|uninstall|start|stop   (Windows) run Docveta as a Windows service
//	docveta version
//
// Configuration comes from environment variables and docveta.conf files; see
// internal/platform/config.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/anand34577/docveta/internal/app"
	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/firstrun"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/platform/builtindb"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/crypto"
)

// version is set at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	if isWindowsService() {
		serviceMode = true
		if err := runAsService(run); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if runtime.GOOS == "windows" && len(os.Args) == 1 && isConsole() {
			// Started by double-click: keep the window open long enough to read the error.
			fmt.Fprintln(os.Stderr, "\nPress Enter to close.")
			_, _ = fmt.Scanln()
		}
		os.Exit(1)
	}
}

// serviceMode is set when Docveta runs as a Windows service: logs then go to a file.
var serviceMode bool

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: lvl}
	var out io.Writer = os.Stdout
	if serviceMode {
		out = openLogFile(cfg.DataDir)
	}
	var h slog.Handler = slog.NewJSONHandler(out, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(out, opts)
	}
	return slog.New(h)
}

// openLogFile opens <data>/logs/docveta.log for appending, keeping one older file.
// Kept simple on purpose: one rotation step at startup. Use the OS log tools for more.
func openLogFile(dataDir string) io.Writer {
	dir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(dir, 0o750)
	path := filepath.Join(dir, "docveta.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 20<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return os.Stderr
	}
	return f
}

func isConsole() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func run(parent context.Context, args []string) error {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "version":
		fmt.Println("docveta", version)
		return nil
	case "service":
		return serviceCmd(args)
	}
	if serviceMode {
		// Services start in C:\Windows\System32; resolve relative paths (./data) next to the program.
		if exe, err := os.Executable(); err == nil {
			_ = os.Chdir(filepath.Dir(exe))
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration:\n%w", err)
	}
	log := newLogger(cfg)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	stopDB, err := useBuiltinDB(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() { stopDB() }()
	if cmd != "serve" && cfg.DatabaseURL == "" {
		return errors.New("Docveta isn't connected to a database yet: run `docveta serve` and finish the setup page in your browser, or set DOCVETA_DATABASE_URL")
	}
	switch cmd {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		noMigrate := fs.Bool("no-migrate", false, "don't apply database migrations on start")
		_ = fs.Parse(args)
		if cfg.Interactive && !serviceMode {
			fmt.Printf("\n  Docveta %s is starting. Open %s in your browser.\n  Data folder: %s\n  Press Ctrl+C to stop.\n\n", version, cfg.BaseURL, cfg.DataDir)
			openBrowser(cfg.BaseURL.String())
		}
		if cfg.DatabaseURL == "" {
			dbURL, err := firstrun.Run(ctx, cfg, log)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			cfg.DatabaseURL = dbURL
			if stopDB, err = useBuiltinDB(ctx, cfg, log); err != nil {
				return err
			}
		}
		if err := waitForDatabase(ctx, cfg, log); err != nil {
			return nil // stopped while waiting
		}
		a, err := app.New(ctx, cfg, log, version, *noMigrate)
		if err != nil {
			return err
		}
		defer a.Close()
		return a.Serve(ctx)
	case "migrate":
		a, err := app.New(ctx, cfg, log, version, false)
		if err != nil {
			return err
		}
		a.Close()
		fmt.Println("migrations applied")
		return nil
	case "user":
		return userCmd(ctx, cfg, log, args)
	case "doctor":
		return doctor(ctx, cfg, args)
	}
	return fmt.Errorf("unknown command %q (try: serve, migrate, user, doctor, version)", cmd)
}

func userCmd(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: docveta user create|reset-password ...")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("user", flag.ExitOnError)
	email := fs.String("email", "", "email address")
	name := fs.String("name", "", "display name")
	isAdmin := fs.Bool("admin", false, "make the user an administrator")
	_ = fs.Parse(args)
	password := os.Getenv("DOCVETA_PASSWORD")
	if *email == "" || password == "" {
		return errors.New("--email and the DOCVETA_PASSWORD environment variable are required")
	}
	a, err := app.New(ctx, cfg, log, version, false)
	if err != nil {
		return err
	}
	defer a.Close()
	sys := auth.System()
	switch sub {
	case "create":
		if *name == "" {
			*name, _, _ = strings.Cut(*email, "@")
		}
		u, err := a.Identity.CreateUser(ctx, sys, identity.NewUser{Email: *email, DisplayName: *name, Password: &password, IsAdmin: *isAdmin})
		if err != nil {
			return err
		}
		fmt.Println("created user", u.Email, u.ID)
	case "reset-password":
		hash, err := crypto.HashPassword(password)
		if err != nil {
			return err
		}
		tag, err := a.Pool.Exec(ctx, `UPDATE users SET password_hash=$2, status='active', updated_at=now() WHERE email=$1`, strings.ToLower(*email), hash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("no user with that email")
		}
		if _, err := a.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=(SELECT id FROM users WHERE email=$1) AND revoked_at IS NULL`, strings.ToLower(*email)); err != nil {
			return err
		}
		fmt.Println("password reset for", *email)
	default:
		return fmt.Errorf("unknown user command %q", sub)
	}
	return nil
}

// useBuiltinDB starts Docveta's own PostgreSQL when the setup page chose it
// (DOCVETA_DATABASE_URL=builtin) and points cfg at it. The returned function stops it.
func useBuiltinDB(ctx context.Context, cfg *config.Config, log *slog.Logger) (func(), error) {
	if cfg.DatabaseURL != builtindb.Setting {
		return func() {}, nil
	}
	dbURL, stop, err := builtindb.Start(ctx, cfg.DataDir, log)
	if err != nil {
		return nil, fmt.Errorf("built-in database: %w", err)
	}
	cfg.DatabaseURL = dbURL
	return stop, nil
}

// waitForDatabase retries until PostgreSQL answers. At boot the database service may
// start after Docveta; failing immediately would leave Docveta down.
func waitForDatabase(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	for attempt := 0; ; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := pgx.Connect(cctx, cfg.DatabaseURL)
		cancel()
		if err == nil {
			conn.Close(context.Background())
			return nil
		}
		if attempt%12 == 0 { // every minute
			log.Warn("waiting for the database", "err", err,
				"hint", "start PostgreSQL, or fix DOCVETA_DATABASE_URL in "+config.DataFile(cfg.DataDir)+" (delete that line to get the setup page again)")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// openBrowser opens url on desktop systems when Docveta is started by hand.
func openBrowser(url string) {
	if os.Getenv("DOCVETA_OPEN_BROWSER") == "false" {
		return
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		return // servers usually have no desktop; the console shows the address
	}
	go func() {
		time.Sleep(1500 * time.Millisecond) // let the server start listening
		_ = c.Run()
	}()
}
