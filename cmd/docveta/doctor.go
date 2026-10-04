package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anand34577/docveta/internal/app"
	"github.com/anand34577/docveta/internal/platform/config"
	"github.com/anand34577/docveta/internal/platform/crypto"
	"github.com/anand34577/docveta/internal/platform/db"
	"github.com/anand34577/docveta/internal/platform/settings"
	"github.com/anand34577/docveta/internal/storage"
)

type doctorReport struct{ failed int }

func (d *doctorReport) add(level, msg string, a ...any) {
	if level == "FAIL" {
		d.failed++
	}
	fmt.Printf("%-4s  %s\n", level, fmt.Sprintf(msg, a...))
}

func (d *doctorReport) result() error {
	if d.failed > 0 {
		return fmt.Errorf("%d check(s) failed", d.failed)
	}
	fmt.Println("\nAll checks passed.")
	return nil
}

// doctor checks an installation without changing it (DESIGN §23.3). It fails if any
// check failed; warnings don't fail it.
func doctor(ctx context.Context, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	verifyBlobs := fs.Bool("verify-blobs", false, "read every stored file and verify its checksum (slow)")
	_ = fs.Parse(args)
	r := &doctorReport{}

	// Configuration
	if h := cfg.BaseURL.Hostname(); cfg.BaseURL.Scheme != "https" && h != "localhost" && h != "127.0.0.1" {
		r.add("WARN", "DOCVETA_BASE_URL is %s: use https so session cookies are marked Secure", cfg.BaseURL)
	} else {
		r.add("OK", "base URL %s", cfg.BaseURL)
	}
	if cfg.DevMode {
		r.add("WARN", "DOCVETA_DEV=true relaxes cookie and CSP protections; don't use it in production")
	}

	// Storage
	store, scratch, err := app.OpenStore(ctx, cfg)
	if err != nil {
		r.add("FAIL", "storage: %v", err)
		store = nil
	} else {
		if cfg.Storage == "s3" {
			r.add("OK", "documents are stored in the S3 bucket %q at %s", cfg.S3.Bucket, cfg.S3.Endpoint)
		}
		probe := filepath.Join(scratch.TempDir(), ".doctor-probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
			r.add("FAIL", "data directory %s is not writable: %v", cfg.DataDir, err)
		} else {
			_ = os.Remove(probe)
			r.add("OK", "data directory %s is writable", cfg.DataDir)
		}
		switch free := scratch.FreeBytes(); {
		case free < 0:
			r.add("WARN", "couldn't determine free disk space")
		case free < 5<<30:
			r.add("WARN", "only %.1f GB free in the data directory", float64(free)/(1<<30))
		default:
			r.add("OK", "%.1f GB free in the data directory", float64(free)/(1<<30))
		}
	}

	// Database
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		r.add("FAIL", "database: %v", err)
		return r.result()
	}
	defer pool.Close()
	var serverVersion int
	var dbNow time.Time
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int, now()`).Scan(&serverVersion, &dbNow); err != nil {
		r.add("FAIL", "database query: %v", err)
		return r.result()
	}
	if serverVersion < 160000 {
		r.add("FAIL", "PostgreSQL %d is too old; 16 or newer is required", serverVersion/10000)
	} else {
		r.add("OK", "PostgreSQL %d", serverVersion/10000)
	}
	var trgm bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name='pg_trgm')`).Scan(&trgm)
	if !trgm {
		r.add("FAIL", "extension pg_trgm is not available on the database server (install the postgresql contrib package)")
	} else {
		r.add("OK", "extension pg_trgm available")
	}
	if skew := time.Since(dbNow).Abs(); skew > 30*time.Second {
		r.add("WARN", "clock differs from the database server by %s; sync both with NTP", skew.Round(time.Second))
	} else {
		r.add("OK", "clock in sync with the database")
	}
	cur, target, err := db.MigrationVersions(ctx, pool)
	switch {
	case err != nil:
		r.add("FAIL", "migrations: %v", err)
	case cur < target:
		r.add("WARN", "schema version %d, %d available: run `docveta migrate` (serve does it on start)", cur, target)
	case cur > target:
		r.add("FAIL", "schema version %d is newer than this binary knows (%d): was Docveta downgraded?", cur, target)
	default:
		r.add("OK", "schema up to date (version %d)", cur)
	}
	if cur == target && cur > 0 {
		doctorData(ctx, cfg, pool, store, *verifyBlobs, r)
	}
	return r.result()
}

// doctorData runs the checks that need an up-to-date schema.
func doctorData(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, store storage.Store, verifyBlobs bool, r *doctorReport) {
	// A wrong DOCVETA_SECRET_KEY makes stored secrets (SMTP, OIDC) undecryptable.
	var key string
	if err := pool.QueryRow(ctx, `SELECT key FROM secret_settings LIMIT 1`).Scan(&key); err == nil {
		keys, err := crypto.NewKeys(cfg.SecretKey)
		if err == nil {
			_, err = settings.New(pool, keys).GetSecret(ctx, key)
		}
		if err != nil {
			r.add("FAIL", "stored secrets can't be decrypted: DOCVETA_SECRET_KEY differs from the one used to save them")
		} else {
			r.add("OK", "DOCVETA_SECRET_KEY decrypts stored secrets")
		}
	}

	var enabled, online int
	_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE enabled),
		count(*) FILTER (WHERE enabled AND last_seen_at > now() - interval '5 minutes') FROM workers`).Scan(&enabled, &online)
	switch {
	case enabled == 0:
		r.add("INFO", "no OCR workers configured: scans won't be searchable (born-digital PDFs are)")
	case online < enabled:
		r.add("WARN", "%d of %d OCR workers online", online, enabled)
	default:
		r.add("OK", "%d OCR worker(s) online", online)
	}

	if store == nil {
		return
	}
	refs, err := app.ReferencedBlobs(ctx, pool)
	if err != nil {
		r.add("FAIL", "list files: %v", err)
		return
	}
	total := len(refs)
	var missing, corrupt int
	for k := range refs {
		if !verifyBlobs {
			if ok, err := store.Exists(ctx, k); err != nil || !ok {
				missing++
				fmt.Printf("      missing: %s\n", k)
			}
			continue
		}
		f, _, err := store.Open(ctx, k)
		if err != nil {
			missing++
			fmt.Printf("      missing: %s\n", k)
			continue
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil || storage.KeyFor(h.Sum(nil)) != k { // keys are content hashes
			corrupt++
			fmt.Printf("      corrupt: %s\n", k)
		}
	}
	what := "present"
	if verifyBlobs {
		what = "present and intact"
	}
	if missing+corrupt > 0 {
		r.add("FAIL", "%d of %d stored files missing, %d corrupt: restore them from backup", missing, total, corrupt)
	} else {
		r.add("OK", "all %d stored files %s", total, what)
	}
}
