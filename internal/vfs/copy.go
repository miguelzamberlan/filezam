package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Conflict policy when a destination exists.
type Conflict string

const (
	ConflictRename    Conflict = "rename"
	ConflictOverwrite Conflict = "overwrite"
	ConflictSkip      Conflict = "skip"
)

// Progress receives copy progress. Any method may be nil.
type Progress struct {
	Add     func(files int, bytes int64)
	Current func(path string)
	Warn    func(path string, err error)
}

func (p *Progress) add(files int, bytes int64) {
	if p != nil && p.Add != nil {
		p.Add(files, bytes)
	}
}
func (p *Progress) current(path string) {
	if p != nil && p.Current != nil {
		p.Current(path)
	}
}
func (p *Progress) warn(path string, err error) {
	if p != nil && p.Warn != nil {
		p.Warn(path, err)
	}
}

// Totals describes a tree.
type Totals struct {
	Files int   `json:"files"`
	Dirs  int   `json:"dirs"`
	Bytes int64 `json:"bytes"`
}

// ErrScanLimit signals that ScanLimited stopped early.
var ErrScanLimit = errors.New("scan limit reached")

// ScanLimited counts files, dirs and bytes under p, stopping with ErrScanLimit
// after maxEntries entries so huge trees do not block a request.
func (r *Root) ScanLimited(ctx context.Context, p string, maxEntries int) (Totals, error) {
	var t Totals
	n := 0
	err := r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
		if path == p && fi.IsDir() {
			return nil
		}
		n++
		if n > maxEntries {
			return ErrScanLimit
		}
		switch {
		case fi.IsDir():
			t.Dirs++
		case fi.Mode().IsRegular():
			t.Files++
			t.Bytes += fi.Size()
		}
		return nil
	})
	return t, err
}

// Scan walks a path and counts files and bytes (symlinks and special files ignored).
func (r *Root) Scan(ctx context.Context, p string) (Totals, error) {
	var t Totals
	err := r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
		if fi.Mode().IsRegular() {
			t.Files++
			t.Bytes += fi.Size()
		}
		return nil
	})
	return t, err
}

// walk calls fn for p and, when p is a directory, all descendants (lstat, no symlink following).
// Errors opening p itself are returned; a descendant directory that cannot be opened or read
// (host-side permissions) is reported to fn and then skipped, so one unreadable folder never
// aborts the index, a search, a quota scan or a zip. Descent stops at WalkMaxDepth.
func (r *Root) walk(ctx context.Context, p string, fn func(path string, fi fs.FileInfo) error) error {
	fi, err := r.r.Lstat(osPath(p))
	if err != nil {
		return MapError(err)
	}
	if err := fn(p, fi); err != nil {
		return err
	}
	if !fi.IsDir() {
		return nil
	}
	return r.walkDir(ctx, p, Depth(p), fn, true)
}

func (r *Root) walkDir(ctx context.Context, p string, depth int, fn func(path string, fi fs.FileInfo) error, top bool) error {
	f, err := r.r.Open(osPath(p))
	if err != nil {
		if top {
			return MapError(err)
		}
		return nil
	}
	des, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		if top {
			return MapError(err)
		}
		return nil
	}
	for _, de := range des {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasPrefix(de.Name(), ReservedPrefix) {
			continue // partes de upload e lixeira nunca são contadas, copiadas ou zipadas
		}
		child := Join(p, de.Name())
		cfi, err := de.Info()
		if err != nil {
			continue
		}
		if err := fn(child, cfi); err != nil {
			return err
		}
		if cfi.IsDir() && depth+1 < WalkMaxDepth {
			if err := r.walkDir(ctx, child, depth+1, fn, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveDest computes the destination path for copying/moving src into dstDir,
// applying the conflict policy. ok=false means skip.
func (r *Root) ResolveDest(src, dstDir string, policy Conflict) (dst string, ok bool, err error) {
	name := Base(src)
	dst = Join(dstDir, name)
	exists, err := r.Exists(dst)
	if err != nil {
		return "", false, err
	}
	if !exists {
		return dst, true, nil
	}
	switch policy {
	case ConflictSkip:
		return dst, false, nil
	case ConflictOverwrite:
		return dst, true, nil
	default:
		u, err := r.UniqueName(dstDir, name)
		if err != nil {
			return "", false, err
		}
		return Join(dstDir, u), true, nil
	}
}

// CopyTree copies src (file or directory) to dst. dst must not exist unless
// policy is overwrite (files are replaced, directories merged) or rename
// (per-entry renaming inside merged directories).
func (r *Root) CopyTree(ctx context.Context, src, dst string, policy Conflict, prog *Progress) error {
	if src == "" {
		return ErrRootOp
	}
	if IsWithin(src, dst) {
		return ErrNested
	}
	fi, err := r.r.Lstat(src)
	if err != nil {
		return MapError(err)
	}
	return r.copyEntry(ctx, src, dst, fi, policy, prog)
}

func (r *Root) copyEntry(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mode := fi.Mode()
	switch {
	case mode.IsDir():
		return r.copyDir(ctx, src, dst, fi, policy, prog)
	case mode.IsRegular():
		return r.copyFile(ctx, src, dst, fi, policy, prog)
	default:
		prog.warn(src, errors.New("symlink or special file skipped"))
		return nil
	}
}

func (r *Root) copyDir(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress) error {
	if err := r.r.Mkdir(dst, fi.Mode().Perm()|0o700); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return MapError(err)
		}
		dfi, serr := r.r.Lstat(dst)
		if serr != nil || !dfi.IsDir() {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
	}
	f, err := r.r.Open(src)
	if err != nil {
		return MapError(err)
	}
	des, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return MapError(err)
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ReservedPrefix) {
			continue
		}
		cfi, err := de.Info()
		if err != nil {
			continue
		}
		csrc := Join(src, de.Name())
		cdst := Join(dst, de.Name())
		if exists, err := r.Exists(cdst); err != nil {
			return err
		} else if exists {
			switch policy {
			case ConflictSkip:
				if cfi.IsDir() {
					// merge directories even when skipping files
					if err := r.copyEntry(ctx, csrc, cdst, cfi, policy, prog); err != nil {
						return err
					}
				}
				continue
			case ConflictRename:
				if !cfi.IsDir() {
					u, err := r.UniqueName(dst, de.Name())
					if err != nil {
						return err
					}
					cdst = Join(dst, u)
				}
			}
		}
		if err := r.copyEntry(ctx, csrc, cdst, cfi, policy, prog); err != nil {
			return err
		}
	}
	_ = r.r.Chtimes(dst, time.Time{}, fi.ModTime())
	return nil
}

const copyChunk = 32 << 20

func (r *Root) copyFile(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress) error {
	prog.current(src)
	in, err := r.r.Open(src)
	if err != nil {
		return MapError(err)
	}
	defer in.Close()
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if policy == ConflictOverwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	out, err := r.r.OpenFile(dst, flags, fi.Mode().Perm()|0o600)
	if err != nil {
		return MapError(err)
	}
	for {
		if err := ctx.Err(); err != nil {
			out.Close()
			_ = r.r.Remove(dst)
			return err
		}
		n, err := io.CopyN(out, in, copyChunk)
		prog.add(0, n)
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Close()
			_ = r.r.Remove(dst)
			return MapError(err)
		}
	}
	if err := out.Close(); err != nil {
		_ = r.r.Remove(dst)
		return MapError(err)
	}
	_ = r.r.Chtimes(dst, time.Time{}, fi.ModTime())
	prog.add(1, 0)
	return nil
}

// RemoveTree deletes a tree reporting progress per regular file.
func (r *Root) RemoveTree(ctx context.Context, p string, prog *Progress) error {
	if p == "" {
		return ErrRootOp
	}
	fi, err := r.r.Lstat(p)
	if err != nil {
		return MapError(err)
	}
	if !fi.IsDir() {
		if err := r.r.Remove(p); err != nil {
			return MapError(err)
		}
		prog.add(1, 0)
		return nil
	}
	f, err := r.r.Open(p)
	if err != nil {
		return MapError(err)
	}
	des, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return MapError(err)
	}
	for _, de := range des {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.RemoveTree(ctx, Join(p, de.Name()), prog); err != nil {
			return err
		}
	}
	return MapError(r.r.Remove(p))
}
