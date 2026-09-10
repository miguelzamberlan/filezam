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

// identity returns (device, inode) of fi, or zeros when unavailable.
func identity(fi os.FileInfo) (uint64, uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), st.Ino
	}
	return 0, 0
}

func diskUsage(path string) DiskUsage {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}
	}
	bs := uint64(st.Bsize)
	return DiskUsage{Total: st.Blocks * bs, Free: st.Bavail * bs}
}
