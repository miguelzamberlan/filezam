// Package vfs implements all filesystem access through an os.Root sandbox.
package vfs

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
)

// Sentinel errors mapped from OS errors so handlers can produce stable codes.
var (
	ErrNotFound     = errors.New("not found")
	ErrExists       = errors.New("already exists")
	ErrIsDir        = errors.New("is a directory")
	ErrNotDir       = errors.New("not a directory")
	ErrNoSpace      = errors.New("no space left on device")
	ErrInvalidPath  = errors.New("invalid path")
	ErrInvalidName  = errors.New("invalid name")
	ErrEscape       = errors.New("path escapes root")
	ErrCrossDevice  = errors.New("cross-device")
	ErrNested       = errors.New("destination is inside source")
	ErrRootOp       = errors.New("operation not allowed on root")
	ErrPermission   = errors.New("permission denied")
	ErrTooManyLinks = errors.New("too many symlinks")
)

// MapError converts OS errors into vfs sentinel errors (wrapping the original).
func MapError(err error) error {
	if err == nil {
		return nil
	}
	for _, s := range []error{ErrNotFound, ErrExists, ErrIsDir, ErrNotDir, ErrNoSpace, ErrInvalidPath, ErrInvalidName, ErrEscape, ErrCrossDevice, ErrNested, ErrRootOp, ErrPermission, ErrTooManyLinks} {
		if errors.Is(err, s) {
			return err
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.Join(ErrNotFound, err)
	case errors.Is(err, fs.ErrExist):
		return errors.Join(ErrExists, err)
	case errors.Is(err, fs.ErrPermission):
		return errors.Join(ErrPermission, err)
	case errors.Is(err, syscall.EISDIR):
		return errors.Join(ErrIsDir, err)
	case errors.Is(err, syscall.ENOTDIR):
		return errors.Join(ErrNotDir, err)
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return errors.Join(ErrNoSpace, err)
	case errors.Is(err, syscall.EXDEV):
		return errors.Join(ErrCrossDevice, err)
	case errors.Is(err, syscall.ELOOP):
		return errors.Join(ErrTooManyLinks, err)
	case errors.Is(err, syscall.ENOTEMPTY):
		return errors.Join(ErrExists, err)
	case errors.Is(err, syscall.ENAMETOOLONG):
		return errors.Join(ErrInvalidName, err)
	}
	msg := err.Error()
	if strings.Contains(msg, "escapes from parent") {
		return errors.Join(ErrEscape, err)
	}
	return err
}
