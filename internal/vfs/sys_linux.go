//go:build linux

package vfs

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func preallocate(f *os.File, size int64) error {
	err := unix.Fallocate(int(f.Fd()), 0, 0, size)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return f.Truncate(size)
	}
	return err
}

func diskFree(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}
