//go:build !linux

package vfs

import "os"

func preallocate(f *os.File, size int64) error { return f.Truncate(size) }

func diskFree(path string) uint64 { return 0 }
