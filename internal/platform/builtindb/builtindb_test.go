package builtindb

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/ulikunitz/xz"
)

func jarWith(t *testing.T, entries map[string]string, links map[string]string) []byte {
	t.Helper()
	var tb bytes.Buffer
	xw, _ := xz.NewWriter(&tb)
	tw := tar.NewWriter(xw)
	for name, body := range entries {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	for name, target := range links {
		_ = tw.WriteHeader(&tar.Header{Name: name, Linkname: target, Typeflag: tar.TypeSymlink})
	}
	_ = tw.Close()
	_ = xw.Close()
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, _ := zw.Create("postgres-test.txz")
	_, _ = io.Copy(w, &tb)
	_ = zw.Close()
	return zb.Bytes()
}

func TestVerifyRejectsWrongChecksum(t *testing.T) {
	if err := verify([]byte("x"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("a wrong checksum was accepted")
	}
	if err := verify([]byte("x"), "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"); err != nil {
		t.Fatal(err)
	}
}

func TestExtract(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	if err := extract(jarWith(t, map[string]string{"bin/initdb": "x", "share/a.control": "y"}, nil), dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "bin", "initdb")); string(b) != "x" {
		t.Fatalf("got %q", b)
	}
}

func TestExtractRejectsUnsafePaths(t *testing.T) {
	if err := extract(jarWith(t, map[string]string{"../evil": "x"}, nil), t.TempDir()); err == nil {
		t.Fatal("a path outside the target was accepted")
	}
	if err := extract(jarWith(t, nil, map[string]string{"lib/x.so": "/etc/passwd"}), t.TempDir()); err == nil {
		t.Fatal("an absolute symlink was accepted")
	}
}

// TestStartRealPostgres downloads PostgreSQL and runs it (about 15–60 MB, a minute):
//
//	DOCVETA_TEST_BUILTIN=1 go test ./internal/platform/builtindb/ -run Real -v
func TestStartRealPostgres(t *testing.T) {
	if os.Getenv("DOCVETA_TEST_BUILTIN") == "" {
		t.Skip("set DOCVETA_TEST_BUILTIN=1 to download and run PostgreSQL")
	}
	ctx := context.Background()
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	for round := 1; round <= 2; round++ { // the second round reuses the binaries and cluster
		dbURL, stop, err := Start(ctx, dir, log)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pgx.Connect(ctx, dbURL)
		if err != nil {
			stop()
			t.Fatal(err)
		}
		for _, ext := range []string{"pg_trgm", "citext"} {
			if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS "+ext); err != nil {
				t.Errorf("round %d: %s: %v", round, ext, err)
			}
		}
		var v string
		_ = conn.QueryRow(ctx, "SHOW server_version").Scan(&v)
		t.Logf("round %d: PostgreSQL %s at %s", round, v, strings.SplitN(dbURL, "@", 2)[1])
		conn.Close(ctx)
		stop()
	}
}
