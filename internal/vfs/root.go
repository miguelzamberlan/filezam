package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Root is a sandboxed directory. All paths passed to its methods must be
// normalized (see Normalize) and are interpreted relative to the root.
type Root struct {
	r      *os.Root
	shared bool
}

// Open opens the base root directory.
func Open(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, MapError(err)
	}
	return &Root{r: r}, nil
}

// Sub opens a nested root at rel (normalized). rel=="" returns a shared view of r.
func (r *Root) Sub(rel string) (*Root, error) {
	if rel == "" {
		return &Root{r: r.r, shared: true}, nil
	}
	sub, err := r.r.OpenRoot(rel)
	if err != nil {
		return nil, MapError(err)
	}
	return &Root{r: sub}, nil
}

// Close releases the root unless it is a shared view.
func (r *Root) Close() error {
	if r.shared {
		return nil
	}
	return r.r.Close()
}

// Name returns the absolute directory name.
func (r *Root) Name() string { return r.r.Name() }

// Entry describes a directory entry.
type Entry struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // file | dir | other
	Size        int64  `json:"size"`
	Mtime       int64  `json:"mtime"` // unix ms
	Link        bool   `json:"link,omitempty"`
	NameInvalid bool   `json:"nameInvalid,omitempty"`
}

// Listing is the result of List.
type Listing struct {
	Entries []Entry
	// Parts are reserved upload part file names found in the directory.
	Parts []string
}

func entryFromInfo(r *Root, dir string, fi fs.FileInfo) Entry {
	e := Entry{Name: fi.Name(), Size: fi.Size(), Mtime: fi.ModTime().UnixMilli()}
	mode := fi.Mode()
	switch {
	case mode.IsDir():
		e.Type = "dir"
		e.Size = 0
	case mode.IsRegular():
		e.Type = "file"
	case mode&fs.ModeSymlink != 0:
		e.Link = true
		e.Type = "other"
		e.Size = 0
		if ti, err := r.r.Stat(osPath(Join(dir, fi.Name()))); err == nil {
			if ti.IsDir() {
				e.Type = "dir"
			} else if ti.Mode().IsRegular() {
				e.Type = "file"
				e.Size = ti.Size()
				e.Mtime = ti.ModTime().UnixMilli()
			}
		}
	default:
		e.Type = "other"
		e.Size = 0
	}
	return e
}

// List reads a directory.
func (r *Root) List(p string) (*Listing, error) {
	f, err := r.r.Open(osPath(p))
	if err != nil {
		return nil, MapError(err)
	}
	defer f.Close()
	des, err := f.ReadDir(-1)
	if err != nil {
		return nil, MapError(err)
	}
	out := &Listing{Entries: make([]Entry, 0, len(des))}
	for _, de := range des {
		name := de.Name()
		if len(name) >= len(ReservedPrefix) && name[:len(ReservedPrefix)] == ReservedPrefix {
			out.Parts = append(out.Parts, name)
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue // vanished between readdir and lstat
		}
		e := entryFromInfo(r, p, fi)
		if !validUTF8(name) {
			e.NameInvalid = true
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

// Stat returns the entry for a path.
func (r *Root) Stat(p string) (*Entry, error) {
	fi, err := r.r.Lstat(osPath(p))
	if err != nil {
		return nil, MapError(err)
	}
	e := entryFromInfo(r, Dir(p), fi)
	if p == "" {
		e.Name = ""
		e.Type = "dir"
	}
	return &e, nil
}

// Exists reports whether p exists (without following a final symlink).
func (r *Root) Exists(p string) (bool, error) {
	_, err := r.r.Lstat(osPath(p))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, MapError(err)
}

// OpenFile opens a regular file for reading (following in-root symlinks).
func (r *Root) OpenFile(p string) (*os.File, fs.FileInfo, error) {
	f, err := r.r.Open(osPath(p))
	if err != nil {
		return nil, nil, MapError(err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, MapError(err)
	}
	if fi.IsDir() {
		f.Close()
		return nil, nil, ErrIsDir
	}
	return f, fi, nil
}

// Mkdir creates a single directory; fails if it exists.
func (r *Root) Mkdir(p string) error {
	if p == "" {
		return ErrRootOp
	}
	return MapError(r.r.Mkdir(p, 0o755))
}

// MkdirAll creates a directory and parents.
func (r *Root) MkdirAll(p string) error {
	if p == "" {
		return nil
	}
	return MapError(r.r.MkdirAll(p, 0o755))
}

// Rename renames an entry in place. Fails if the new name exists.
func (r *Root) Rename(p, newName string) (string, error) {
	if p == "" {
		return "", ErrRootOp
	}
	if err := ValidName(newName); err != nil {
		return "", err
	}
	dst := Join(Dir(p), newName)
	if dst == p {
		return p, nil
	}
	if ok, err := r.Exists(dst); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("%w: %s", ErrExists, newName)
	}
	if err := r.r.Rename(p, dst); err != nil {
		return "", MapError(err)
	}
	return dst, nil
}

// Remove deletes a file or directory tree. Symlinks are removed, not followed.
func (r *Root) Remove(p string) error {
	if p == "" {
		return ErrRootOp
	}
	return MapError(r.r.RemoveAll(p))
}

// Chtimes sets the modification time.
func (r *Root) Chtimes(p string, mtime time.Time) error {
	return MapError(r.r.Chtimes(osPath(p), time.Time{}, mtime))
}

// Move renames src into dstDir keeping its name. See MoveTo.
func (r *Root) Move(src, dstDir string, overwrite bool) (string, error) {
	if src == "" {
		return "", ErrRootOp
	}
	return r.MoveTo(src, Join(dstDir, Base(src)), overwrite)
}

// MoveTo renames src to dst. Fails on an existing target unless overwrite (which
// removes it first). Returns ErrCrossDevice when a rename is impossible so the
// caller can fall back to copy+delete.
func (r *Root) MoveTo(src, dst string, overwrite bool) (string, error) {
	if src == "" || dst == "" {
		return "", ErrRootOp
	}
	if dst == src {
		return dst, nil
	}
	if IsWithin(src, dst) {
		return "", ErrNested
	}
	if fi, err := r.r.Lstat(osPath(Dir(dst))); err != nil {
		return "", MapError(err)
	} else if !fi.IsDir() {
		return "", ErrNotDir
	}
	if exists, err := r.Exists(dst); err != nil {
		return "", err
	} else if exists {
		if !overwrite {
			return "", fmt.Errorf("%w: %s", ErrExists, dst)
		}
		if err := r.Remove(dst); err != nil {
			return "", err
		}
	}
	if err := r.r.Rename(src, dst); err != nil {
		return "", MapError(err)
	}
	return dst, nil
}

// UniqueName finds "name (n).ext" that does not exist in dir.
func (r *Root) UniqueName(dir, name string) (string, error) {
	base, ext := SplitExt(name)
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		ok, err := r.Exists(Join(dir, cand))
		if err != nil {
			return "", err
		}
		if !ok {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%w: no free name", ErrExists)
}

// DiskFree returns free bytes on the filesystem backing the root (0 if unknown).
func (r *Root) DiskFree() uint64 {
	return diskFree(r.r.Name())
}
