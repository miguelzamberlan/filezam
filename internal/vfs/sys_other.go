//go:build !linux

package vfs

import "os"

func preallocate(f *os.File, size int64) error { return f.Truncate(size) }

func diskUsage(path string) DiskUsage { return DiskUsage{} }
