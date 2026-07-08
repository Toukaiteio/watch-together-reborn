//go:build !windows

package main

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

func diskFreeBytes(path string) (int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(abs, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func samePath(a string, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
