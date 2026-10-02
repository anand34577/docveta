// Package storage implements the content-addressed blob store (DESIGN §19.1).
// Blobs are immutable and addressed by SHA-256, which gives de-duplication and
// makes backups consistent: a blob is never modified, only added or (after a
// grace period) garbage collected.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrNotFound = errors.New("blob not found")

// Store is the blob store abstraction. A second implementation (S3) can be added
// without touching callers.
type Store interface {
	// Stage creates a temporary writer; Commit moves it into the store by content hash.
	Stage() (*Staged, error)
	Open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error)
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error
	// Walk lists all blob keys with their modification time (used by GC).
	Walk(ctx context.Context, fn func(key string, mod time.Time) error) error
	// FreeBytes reports free space where blobs are stored, or -1 if unknown.
	FreeBytes() int64
}

// Staged is a temporary file that hashes everything written to it.
type Staged struct {
	f      *os.File
	h      hash.Hash
	size   int64
	store  *FS
	closed bool
}

func (s *Staged) Write(p []byte) (int, error) {
	n, err := s.f.Write(p)
	s.h.Write(p[:n])
	s.size += int64(n)
	return n, err
}

func (s *Staged) Size() int64    { return s.size }
func (s *Staged) Path() string   { return s.f.Name() }
func (s *Staged) SHA256() []byte { return s.h.Sum(nil) }

// Discard removes the staged file.
func (s *Staged) Discard() {
	if !s.closed {
		s.f.Close()
		s.closed = true
	}
	os.Remove(s.f.Name())
}

// Commit fsyncs the staged file and atomically moves it into the store, returning
// its key. If an identical blob already exists the staged copy is discarded.
func (s *Staged) Commit() (key string, err error) {
	defer func() {
		if err != nil {
			s.Discard()
		}
	}()
	if err := s.f.Sync(); err != nil {
		return "", err
	}
	if err := s.f.Close(); err != nil {
		return "", err
	}
	s.closed = true
	key = KeyFor(s.SHA256())
	dst := s.store.path(key)
	if _, err := os.Stat(dst); err == nil {
		os.Remove(s.f.Name())
		return key, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return "", err
	}
	if err := os.Rename(s.f.Name(), dst); err != nil {
		return "", err
	}
	syncDir(filepath.Dir(dst))
	return key, nil
}

// KeyFor returns the blob key for a SHA-256 digest.
func KeyFor(sum []byte) string {
	h := hex.EncodeToString(sum)
	return "sha256/" + h[:2] + "/" + h[2:4] + "/" + h
}

// FS stores blobs on the local filesystem.
type FS struct {
	root string
	tmp  string
}

func NewFS(dataDir string) (*FS, error) {
	root := filepath.Join(dataDir, "blobs")
	tmp := filepath.Join(dataDir, "tmp")
	for _, d := range []string{root, tmp} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	return &FS{root: root, tmp: tmp}, nil
}

// TempDir is a scratch directory on the same filesystem as the store.
func (s *FS) TempDir() string { return s.tmp }

func (s *FS) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key)+".bin")
}

func validKey(key string) bool {
	if !strings.HasPrefix(key, "sha256/") || strings.Contains(key, "..") {
		return false
	}
	return len(key) == len("sha256/ab/cd/")+64
}

func (s *FS) Stage() (*Staged, error) {
	f, err := os.CreateTemp(s.tmp, "upload-*")
	if err != nil {
		return nil, err
	}
	return &Staged{f: f, h: sha256.New(), store: s}, nil
}

func (s *FS) Open(_ context.Context, key string) (io.ReadSeekCloser, int64, error) {
	if !validKey(key) {
		return nil, 0, ErrNotFound
	}
	f, err := os.Open(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (s *FS) Exists(_ context.Context, key string) (bool, error) {
	if !validKey(key) {
		return false, nil
	}
	_, err := os.Stat(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (s *FS) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return ErrNotFound
	}
	err := os.Remove(s.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (s *FS) Walk(ctx context.Context, fn func(key string, mod time.Time) error) error {
	return filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(p, ".bin") {
			return nil
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(strings.TrimSuffix(filepath.ToSlash(rel), ".bin"), info.ModTime())
	})
}

// CleanTemp removes stale temporary files older than maxAge (interrupted uploads).
func (s *FS) CleanTemp(maxAge time.Duration) {
	entries, err := os.ReadDir(s.tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && time.Since(info.ModTime()) > maxAge {
			os.RemoveAll(filepath.Join(s.tmp, e.Name()))
		}
	}
}

// PutBytes stores a small in-memory blob (thumbnails, OCR JSON).
func PutBytes(s Store, b []byte) (key string, sum []byte, err error) {
	st, err := s.Stage()
	if err != nil {
		return "", nil, err
	}
	if _, err := st.Write(b); err != nil {
		st.Discard()
		return "", nil, err
	}
	sum = st.SHA256()
	key, err = st.Commit()
	return key, sum, err
}

// ReadAll reads a whole blob into memory (bounded by max).
func ReadAll(ctx context.Context, s Store, key string, max int64) ([]byte, error) {
	r, size, err := s.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if size > max {
		return nil, fmt.Errorf("blob %s too large (%d bytes)", key, size)
	}
	return io.ReadAll(io.LimitReader(r, max))
}
