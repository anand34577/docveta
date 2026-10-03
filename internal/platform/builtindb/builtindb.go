// Package builtindb runs a private PostgreSQL for Docveta inside its data folder, so a
// fresh install needs no database server: the setup page offers it as the default.
//
// Security: PostgreSQL listens on 127.0.0.1 only (no Unix socket), every connection needs a
// random 64-character password that never leaves the data folder, and the binaries are
// downloaded once from Maven Central and checked against the SHA-256 sums pinned below.
//
// Layout in the data folder:
//
//	postgres/bin-<version>/   PostgreSQL binaries (initdb, pg_ctl, postgres, lib, share)
//	postgres/data/            the database cluster
//	postgres/password         the docveta role's password (0600)
//	postgres/postgres.log     PostgreSQL's own log
package builtindb

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ulikunitz/xz"
)

// Setting is the DOCVETA_DATABASE_URL value that selects the built-in database.
const Setting = "builtin"

// Version of PostgreSQL used. Changing the major version needs a data migration (pg_upgrade).
const Version = "17.11.0"

const user = "docveta"

// artifacts maps GOOS/GOARCH to the zonky.io binary package and its SHA-256.
var artifacts = map[string][2]string{
	"linux/amd64":   {"linux-amd64", "0dd7b72b6f335b8ecfb355fa24c5781e8a93edd09880bb77eb52ebbf29b3e96d"},
	"linux/arm64":   {"linux-arm64v8", "8b042e0ea418b1927d95399207950da0734d27fe01e29503ade3bdc464ad4c2b"},
	"linux/arm":     {"linux-arm32v7", "2bb78b2061748876c59f267fe289c5127767a90b45bc5e890bcb0cb1732fd072"},
	"linux/386":     {"linux-i386", "51fa835af2e824951f7c2cb024909a5375576b92f91c04f4e058956e8e2b3650"},
	"darwin/amd64":  {"darwin-amd64", "d464ff178e9860ba204662ac23fa547504b7fd392392ff2fb92e3fd73b5bdb64"},
	"darwin/arm64":  {"darwin-arm64v8", "a1c2786acb0c398f9b2d76806fc52f5dc8b222cbc8e9383a9b9702084daaf3a5"},
	"windows/amd64": {"windows-amd64", "98040fae18dd9633ff95932125b0cecf0a45a1a9312e216e2a33ad03a31d4251"},
	"windows/arm64": {"windows-amd64", "98040fae18dd9633ff95932125b0cecf0a45a1a9312e216e2a33ad03a31d4251"}, // runs under x64 emulation
}

// Supported reports whether a built-in database is available on this system
// (not on 32-bit Windows: no current PostgreSQL build exists for it).
func Supported() bool {
	_, ok := artifacts[runtime.GOOS+"/"+runtime.GOARCH]
	return ok
}

type paths struct{ dir, bin, data, password, log string }

func pathsFor(dataDir string) paths {
	dir, _ := filepath.Abs(filepath.Join(dataDir, "postgres"))
	return paths{
		dir:      dir,
		bin:      filepath.Join(dir, "bin-"+Version),
		data:     filepath.Join(dir, "data"),
		password: filepath.Join(dir, "password"),
		log:      filepath.Join(dir, "postgres.log"),
	}
}

func (p paths) tool(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(p.bin, "bin", name)
}

// Prepare downloads the binaries and creates the database cluster if needed, without
// leaving PostgreSQL running. The setup page calls it so problems show up there.
func Prepare(ctx context.Context, dataDir string, log *slog.Logger) error {
	if !Supported() {
		return fmt.Errorf("the built-in database isn't available on %s/%s; connect a PostgreSQL server instead", runtime.GOOS, runtime.GOARCH)
	}
	if os.Geteuid() == 0 {
		return errors.New("PostgreSQL refuses to run as root: run Docveta as a normal user (the Linux installer creates a \"docveta\" user)")
	}
	p := pathsFor(dataDir)
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return err
	}
	if err := ensureBinaries(ctx, p, log); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(p.data, "PG_VERSION")); err == nil {
		return nil
	}
	if _, err := os.Stat(p.password); errors.Is(err, os.ErrNotExist) {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		if err := os.WriteFile(p.password, []byte(hex.EncodeToString(b)), 0o600); err != nil {
			return err
		}
	}
	log.Info("creating the built-in database", "dir", p.data)
	_ = os.RemoveAll(p.data) // a half-finished earlier attempt
	if out, err := command(ctx, p.tool("initdb"), "-D", p.data, "-U", user, "--pwfile", p.password,
		"-A", "scram-sha-256", "-E", "UTF8", "--no-locale").CombinedOutput(); err != nil {
		return fmt.Errorf("creating the database failed: %v\n%s", err, out)
	}
	// Our settings live in their own file, rewritten on every start (the port may change).
	f, err := os.OpenFile(filepath.Join(p.data, "postgresql.conf"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n# Docveta: network settings, see docveta.conf\ninclude_if_exists = 'docveta.conf'\n")
	return err
}

// Start starts the built-in PostgreSQL (preparing it first if needed) and returns the
// connection URL and a function that stops it again. If it's already running (another
// Docveta command started it), Start uses it and stop does nothing.
func Start(ctx context.Context, dataDir string, log *slog.Logger) (string, func(), error) {
	if err := Prepare(ctx, dataDir, log); err != nil {
		return "", nil, err
	}
	p := pathsFor(dataDir)
	pw, err := os.ReadFile(p.password)
	if err != nil {
		return "", nil, err
	}
	stop := func() {}
	port := runningPort(p)
	if port == 0 {
		if port, err = freePort(); err != nil {
			return "", nil, err
		}
		conf := fmt.Sprintf("listen_addresses = '127.0.0.1'\nport = %d\nunix_socket_directories = ''\n", port)
		if err := os.WriteFile(filepath.Join(p.data, "docveta.conf"), []byte(conf), 0o600); err != nil {
			return "", nil, err
		}
		log.Info("starting the built-in database", "port", port)
		if out, err := runToFile(command(ctx, p.tool("pg_ctl"), "start", "-D", p.data, "-l", p.log, "-w", "-t", "120", "-s"), p.dir); err != nil {
			return "", nil, fmt.Errorf("the built-in database didn't start: %v %s(details in %s)", err, out, p.log)
		}
		stop = func() {
			sctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if out, err := command(sctx, p.tool("pg_ctl"), "stop", "-D", p.data, "-m", "fast", "-w", "-s").CombinedOutput(); err != nil {
				log.Warn("stopping the built-in database", "err", err, "output", strings.TrimSpace(string(out)))
			}
		}
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(user, string(pw)), Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Path: "/docveta", RawQuery: "sslmode=disable"}
	if err := ensureDatabase(ctx, u); err != nil {
		stop()
		return "", nil, err
	}
	return u.String(), stop, nil
}

// runningPort returns the port of a PostgreSQL already running on this cluster, or 0.
func runningPort(p paths) int {
	if command(context.Background(), p.tool("pg_ctl"), "status", "-D", p.data).Run() != nil {
		return 0
	}
	b, err := os.ReadFile(filepath.Join(p.data, "postmaster.pid")) // line 4: port
	if err != nil {
		return 0
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 4 {
		return 0
	}
	port, _ := strconv.Atoi(strings.TrimSpace(lines[3]))
	return port
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func ensureDatabase(ctx context.Context, u url.URL) error {
	admin := u
	admin.Path = "/postgres"
	conn, err := pgx.Connect(ctx, admin.String())
	if err != nil {
		return fmt.Errorf("connecting to the built-in database: %w", err)
	}
	defer conn.Close(context.Background())
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='docveta')`).Scan(&exists); err != nil || exists {
		return err
	}
	_, err = conn.Exec(ctx, `CREATE DATABASE docveta ENCODING 'UTF8' TEMPLATE template0`)
	return err
}

// runToFile runs cmd with its output in a file rather than a pipe: the server pg_ctl
// starts inherits the output handles, so a pipe would never close and Wait would hang.
func runToFile(cmd *exec.Cmd, dir string) (string, error) {
	f, err := os.CreateTemp(dir, "pg_ctl-*.out")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	cmd.Stdout, cmd.Stderr = f, f
	err = cmd.Run()
	out, _ := os.ReadFile(f.Name())
	return strings.TrimSpace(string(out)), err
}

// command runs a PostgreSQL tool without PG* variables from the environment, which would
// point it at another server.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "PG") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	return cmd
}

// ---------------------------------------------------------------- binaries

var downloadBase = "https://repo1.maven.org/maven2/io/zonky/test/postgres"

func ensureBinaries(ctx context.Context, p paths, log *slog.Logger) error {
	if _, err := os.Stat(p.tool("pg_ctl")); err == nil {
		return nil
	}
	a := artifacts[runtime.GOOS+"/"+runtime.GOARCH]
	src := fmt.Sprintf("%s/embedded-postgres-binaries-%s/%s/embedded-postgres-binaries-%s-%s.jar", downloadBase, a[0], Version, a[0], Version)
	log.Info("downloading PostgreSQL for the built-in database", "version", Version, "from", src)
	jar, err := download(ctx, src, a[1])
	if err != nil {
		return fmt.Errorf("downloading PostgreSQL failed (%v). Check the internet connection, or connect a PostgreSQL server instead", err)
	}
	tmp := p.bin + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := extract(jar, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("unpacking PostgreSQL: %w", err)
	}
	return os.Rename(tmp, p.bin)
}

func download(ctx context.Context, src, wantSHA string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return nil, err
	}
	if err := verify(b, wantSHA); err != nil {
		return nil, err
	}
	return b, nil
}

func verify(b []byte, wantSHA string) error {
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != wantSHA {
		return fmt.Errorf("the download doesn't match the expected checksum (got %s, want %s); refusing to use it", got, wantSHA)
	}
	return nil
}

// extract unpacks the .txz archive inside the zonky jar into dir.
func extract(jar []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(jar), int64(len(jar)))
	if err != nil {
		return err
	}
	var txz *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".txz") {
			txz = f
		}
	}
	if txz == nil {
		return errors.New("no .txz archive in the package")
	}
	rc, err := txz.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	xr, err := xz.NewReader(rc)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	root, _ := filepath.Abs(dir)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(h.Name))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path in archive: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || strings.Contains(h.Linkname, "..") {
				return fmt.Errorf("unsafe link in archive: %s -> %s", h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}
