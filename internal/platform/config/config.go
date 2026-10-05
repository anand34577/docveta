// Package config loads bootstrap configuration from environment variables and
// docveta.conf files (see FileName). Everything a user should be able to change at
// runtime lives in the database (settings table) instead.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// S3 holds the settings of S3-compatible storage (DOCVETA_S3_*).
type S3 struct {
	Endpoint, Bucket, Region, AccessKey, SecretKey, Prefix string
	Insecure, PathStyle                                    bool
}

type Config struct {
	BaseURL     *url.URL
	ListenAddr  string
	DatabaseURL string // empty until set up: `serve` then shows the database setup page
	DataDir     string
	SecretKey   []byte
	// WorkerEnrollKey lets OCR workers fetch their own token (POST /worker/v1/enroll).
	// Empty unless DOCVETA_WORKER_ENROLL_KEY_FILE is set (the Docker setup does).
	WorkerEnrollKey []byte
	TrustedProxies  []netip.Prefix
	MaxUploadBytes  int64
	LogLevel        string
	LogFormat       string // json | text
	DevMode         bool   // relaxes cookie Secure flag and CSP for local development
	SessionIdle     time.Duration
	SessionMax      time.Duration
	TrashRetention  time.Duration
	PDFWorkers      int
	JobWorkers      int
	// AllowLocalTargets lets users point Gotify/ntfy/webhook channels at private
	// network addresses (off by default: SSRF protection).
	AllowLocalTargets bool
	// LocalOCR controls the bundled OCR engine next to the executable: auto | off.
	LocalOCR string
	// Interactive is true when running in a terminal (not as a service or container).
	Interactive bool
	// GotenbergURL enables Office document support (DOCVETA_GOTENBERG_URL). Administrators
	// can also set it in the web app.
	GotenbergURL string
	// Storage selects where documents are kept: "fs" (the data folder, default) or "s3".
	Storage string
	S3      S3
	// WatchRoots limits where watched folders may be (DOCVETA_WATCH_ROOTS); default <data>/watch.
	WatchRoots []string
	// MetricsToken, when set, lets Prometheus scrape /metrics from anywhere with
	// "Authorization: Bearer <token>". Without it /metrics only answers the local network.
	MetricsToken string
}

func Load() (*Config, error) {
	if err := loadFile(primaryFile()); err != nil {
		return nil, fmt.Errorf("read %s: %w", primaryFile(), err)
	}
	if dir, err := filepath.Abs(env("DOCVETA_DATA_DIR", "./data")); err == nil {
		if err := loadFile(DataFile(dir)); err != nil {
			return nil, fmt.Errorf("read %s: %w", DataFile(dir), err)
		}
	}
	interactive := isTerminal()
	logFormat := "json"
	if interactive {
		logFormat = "text"
	}
	c := &Config{
		ListenAddr:        env("DOCVETA_LISTEN", ":8080"),
		DatabaseURL:       os.Getenv("DOCVETA_DATABASE_URL"),
		DataDir:           env("DOCVETA_DATA_DIR", "./data"),
		LogLevel:          env("DOCVETA_LOG_LEVEL", "info"),
		LogFormat:         env("DOCVETA_LOG_FORMAT", logFormat),
		DevMode:           envBool("DOCVETA_DEV", false),
		SessionIdle:       envDuration("DOCVETA_SESSION_IDLE", 30*24*time.Hour),
		SessionMax:        envDuration("DOCVETA_SESSION_MAX", 90*24*time.Hour),
		TrashRetention:    envDuration("DOCVETA_TRASH_RETENTION", 30*24*time.Hour),
		PDFWorkers:        envInt("DOCVETA_PDF_WORKERS", 2),
		JobWorkers:        envInt("DOCVETA_JOB_WORKERS", 4),
		AllowLocalTargets: envBool("DOCVETA_ALLOW_LOCAL_TARGETS", false),
		LocalOCR:          env("DOCVETA_LOCAL_OCR", "auto"),
		GotenbergURL:      env("DOCVETA_GOTENBERG_URL", ""),
		WatchRoots:        splitList(os.Getenv("DOCVETA_WATCH_ROOTS")),
		Storage:           strings.ToLower(env("DOCVETA_STORAGE", "fs")),
		S3: S3{
			Endpoint: env("DOCVETA_S3_ENDPOINT", ""), Bucket: env("DOCVETA_S3_BUCKET", ""), Region: env("DOCVETA_S3_REGION", ""),
			Prefix: env("DOCVETA_S3_PREFIX", ""), Insecure: envBool("DOCVETA_S3_INSECURE", false), PathStyle: envBool("DOCVETA_S3_PATH_STYLE", false),
		},
		Interactive: interactive,
	}
	if t, err := secretFromEnv("DOCVETA_METRICS_TOKEN"); err == nil {
		c.MetricsToken = t
	}
	var errs []error

	base := env("DOCVETA_BASE_URL", "http://localhost:8080")
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("DOCVETA_BASE_URL must be an absolute URL like https://docs.example.com (got %q)", base))
	}
	c.BaseURL = u

	mb := envInt("DOCVETA_MAX_UPLOAD_MB", 500)
	if mb <= 0 {
		errs = append(errs, errors.New("DOCVETA_MAX_UPLOAD_MB must be positive"))
	}
	c.MaxUploadBytes = int64(mb) << 20

	for _, p := range splitList(os.Getenv("DOCVETA_TRUSTED_PROXIES")) {
		if !strings.Contains(p, "/") {
			if a, err := netip.ParseAddr(p); err == nil {
				p = netip.PrefixFrom(a, a.BitLen()).String()
			}
		}
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("DOCVETA_TRUSTED_PROXIES: invalid entry %q", p))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, pfx)
	}

	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		errs = append(errs, fmt.Errorf("DOCVETA_DATA_DIR: %w", err))
	}
	c.DataDir = abs

	// The secret key encrypts stored credentials. Generated on first start and saved in
	// the data folder's docveta.conf, so a fresh install needs no configuration.
	secret, err := ensureSecret(c.DataDir)
	if err != nil {
		errs = append(errs, fmt.Errorf("create %s: %w", DataFile(c.DataDir), err))
	} else if len(secret) < 32 {
		errs = append(errs, errors.New("DOCVETA_SECRET_KEY must be at least 32 characters (or remove it and Docveta generates one). Back it up: it encrypts stored secrets"))
	}
	c.SecretKey = []byte(secret)

	switch c.Storage {
	case "fs":
	case "s3":
		var err error
		if c.S3.AccessKey, err = secretFromEnv("DOCVETA_S3_ACCESS_KEY"); err != nil {
			errs = append(errs, err)
		}
		if c.S3.SecretKey, err = secretFromEnv("DOCVETA_S3_SECRET_KEY"); err != nil {
			errs = append(errs, err)
		}
		if c.S3.Endpoint == "" || c.S3.Bucket == "" {
			errs = append(errs, errors.New("DOCVETA_STORAGE=s3 needs DOCVETA_S3_ENDPOINT and DOCVETA_S3_BUCKET"))
		}
	default:
		errs = append(errs, fmt.Errorf("DOCVETA_STORAGE must be fs or s3 (got %q)", c.Storage))
	}

	// Docker: the database password lives in a generated file, not in the URL or .env.
	if f := os.Getenv("DOCVETA_DATABASE_PASSWORD_FILE"); f != "" && c.DatabaseURL != "" {
		if u, err := withPasswordFromFile(c.DatabaseURL, f); err != nil {
			errs = append(errs, fmt.Errorf("DOCVETA_DATABASE_PASSWORD_FILE: %w", err))
		} else {
			c.DatabaseURL = u
		}
	}
	if f := os.Getenv("DOCVETA_WORKER_ENROLL_KEY_FILE"); f != "" {
		key, err := ensureKeyFile(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("DOCVETA_WORKER_ENROLL_KEY_FILE: %w", err))
		}
		c.WorkerEnrollKey = key
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// SecureCookies reports whether cookies must carry the Secure attribute.
func (c *Config) SecureCookies() bool {
	return c.BaseURL.Scheme == "https"
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isTerminal reports whether stdout is a console (someone started Docveta by hand).
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// secretFromEnv reads a credential from NAME or, preferably, from the file named by NAME_FILE
// (Docker secrets), so it needn't appear in the environment.
func secretFromEnv(name string) (string, error) {
	if f := os.Getenv(name + "_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("%s_FILE: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return os.Getenv(name), nil
}
