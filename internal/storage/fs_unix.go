//go:build !windows

package storage

import (
	"os"

	"golang.org/x/sys/unix"
)

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

func (s *FS) FreeBytes() int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(s.root, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize) //nolint:unconvert
}
