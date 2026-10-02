//go:build windows

package storage

import "golang.org/x/sys/windows"

// Directory fsync is not supported on Windows; NTFS rename is journaled.
func syncDir(string) {}

func (s *FS) FreeBytes() int64 {
	p, err := windows.UTF16PtrFromString(s.root)
	if err != nil {
		return -1
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return -1
	}
	return int64(free)
}
