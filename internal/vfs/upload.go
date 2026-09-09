package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// PartName returns the reserved part file name for an upload id.
func PartName(id string) string { return ReservedPrefix + "upload-" + id + ".part" }

// CreatePart creates a new exclusive part file in dir and preallocates size bytes.
// The caller must Close the returned file.
func (r *Root) CreatePart(dir, id string, size int64) (*os.File, error) {
	p := Join(dir, PartName(id))
	f, err := r.r.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, MapError(err)
	}
	if size > 0 {
		if err := preallocate(f, size); err != nil {
			f.Close()
			_ = r.r.Remove(p)
			return nil, MapError(err)
		}
	}
	return f, nil
}

// OpenPart opens an existing part file for writing.
func (r *Root) OpenPart(dir, id string) (*os.File, error) {
	f, err := r.r.OpenFile(Join(dir, PartName(id)), os.O_WRONLY, 0)
	if err != nil {
		return nil, MapError(err)
	}
	return f, nil
}

// RemovePart deletes a part file (ignoring not-found).
func (r *Root) RemovePart(dir, id string) error {
	err := r.r.Remove(Join(dir, PartName(id)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return MapError(err)
	}
	return nil
}

// RemoveReserved deletes a reserved file by name inside dir.
func (r *Root) RemoveReserved(dir, name string) error {
	err := r.r.Remove(Join(dir, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return MapError(err)
	}
	return nil
}

// Finalize atomically moves a part file to its final name.
// With overwrite=false the operation fails with ErrExists if the target appeared.
func (r *Root) Finalize(dir, id, name string, overwrite bool) error {
	part := Join(dir, PartName(id))
	final := Join(dir, name)
	if overwrite {
		if fi, err := r.r.Lstat(final); err == nil && fi.IsDir() {
			return fmt.Errorf("%w: %s", ErrIsDir, name)
		}
		return MapError(r.r.Rename(part, final))
	}
	err := r.r.Link(part, final)
	switch {
	case err == nil:
		return MapError(r.r.Remove(part))
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%w: %s", ErrExists, name)
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.EMLINK):
		// filesystem without hardlinks: best-effort check then rename
		if ok, err := r.Exists(final); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("%w: %s", ErrExists, name)
		}
		return MapError(r.r.Rename(part, final))
	default:
		return MapError(err)
	}
}
