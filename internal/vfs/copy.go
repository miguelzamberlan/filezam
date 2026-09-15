// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
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
	// Tag entra no nome dos temporários desta operação (CopyTempPrefix). É o que permite, depois
	// de uma queda do processo, achar e apagar só o que esta operação deixou pela metade.
	Tag string
}

func (p *Progress) tag() string {
	if p == nil {
		return ""
	}
	return p.Tag
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
	// Em ordem de nome, não na ordem do diretório: o zip dividido em partes depende de a mesma
	// árvore ser percorrida sempre na mesma sequência (ver ZipOrder).
	slices.SortFunc(des, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
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
// Um item de nome equivalente (outra caixa) conta como existente; substituir grava sobre ele.
func (r *Root) ResolveDest(src, dstDir string, policy Conflict) (dst string, ok bool, err error) {
	names := r.NewNamer()
	name := Base(src)
	cur, err := names.Lookup(dstDir, name)
	if err != nil {
		return "", false, err
	}
	if cur == "" {
		return Join(dstDir, name), true, nil
	}
	switch policy {
	case ConflictSkip:
		return Join(dstDir, cur), false, nil
	case ConflictOverwrite:
		return Join(dstDir, cur), true, nil
	default:
		u, err := names.nextFree(dstDir, name)
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
	return r.copyEntry(ctx, src, dst, fi, policy, prog, map[[2]uint64]bool{}, r.NewNamer())
}

// made guarda (dispositivo, inode) das pastas criadas por esta cópia. Se o destino passa por
// um symlink que aponta para dentro da origem (criado fora do app), a pasta recém-criada
// aparece na listagem da origem; sem essa marca a cópia desceria nela sem fim.
func (r *Root) copyEntry(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress, made map[[2]uint64]bool, names *Namer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mode := fi.Mode()
	switch {
	case mode.IsDir():
		return r.copyDir(ctx, src, dst, fi, policy, prog, made, names)
	case mode.IsRegular():
		return r.copyFile(ctx, src, dst, fi, policy, prog)
	default:
		prog.warn(src, errors.New("symlink or special file skipped"))
		return nil
	}
}

func (r *Root) copyDir(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress, made map[[2]uint64]bool, names *Namer) error {
	if err := r.r.Mkdir(dst, fi.Mode().Perm()|0o700); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return MapError(err)
		}
		dfi, serr := r.r.Lstat(dst)
		if serr != nil || !dfi.IsDir() {
			return fmt.Errorf("%w: %s", ErrExists, dst)
		}
	}
	if dfi, err := r.r.Stat(dst); err == nil {
		if dev, ino := identity(dfi); ino != 0 {
			made[[2]uint64{dev, ino}] = true
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
		if cfi.IsDir() {
			if dev, ino := identity(cfi); ino != 0 && made[[2]uint64{dev, ino}] {
				prog.warn(Join(src, de.Name()), errors.New("destination inside the source (symlink) skipped"))
				continue
			}
		}
		csrc := Join(src, de.Name())
		cdst := Join(dst, de.Name())
		// Nome equivalente no destino (outra caixa) é o mesmo item: pastas se juntam e arquivos
		// seguem a política sobre o que já está lá, com o nome de lá.
		if cur, err := names.Lookup(dst, de.Name()); err != nil {
			return err
		} else if cur != "" {
			cdst = Join(dst, cur)
			switch policy {
			case ConflictSkip:
				if cfi.IsDir() {
					// merge directories even when skipping files
					if err := r.copyEntry(ctx, csrc, cdst, cfi, policy, prog, made, names); err != nil {
						return err
					}
				}
				continue
			case ConflictRename:
				if !cfi.IsDir() {
					u, err := names.nextFree(dst, de.Name())
					if err != nil {
						return err
					}
					cdst = Join(dst, u)
				}
			}
		}
		if err := r.copyEntry(ctx, csrc, cdst, cfi, policy, prog, made, names); err != nil {
			return err
		}
		names.Add(dst, Base(cdst))
	}
	_ = r.r.Chtimes(dst, time.Time{}, fi.ModTime())
	return nil
}

const copyChunk = 32 << 20

// CopyTempPrefix is the name prefix of the temporaries a copy tagged tag writes.
func CopyTempPrefix(tag string) string { return ReservedPrefix + "copy-" + tag + "-" }

// copyFile grava num temporário oculto ao lado do destino e só publica no fim. Um arquivo pela
// metade nunca aparece com o nome legítimo — nem se o processo cair no meio, caso em que sobra
// só o temporário, que RemoveTemps recolhe pelo Tag —, e substituir não destrói o original antes
// de a cópia nova estar inteira.
func (r *Root) copyFile(ctx context.Context, src, dst string, fi fs.FileInfo, policy Conflict, prog *Progress) error {
	prog.current(src)
	in, err := r.r.Open(src)
	if err != nil {
		return MapError(err)
	}
	defer in.Close()
	suffix, err := randHex(6)
	if err != nil {
		return err
	}
	tmp := Join(Dir(dst), CopyTempPrefix(prog.tag())+suffix)
	out, err := r.r.OpenFile(osPath(tmp), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm()|0o600)
	if err != nil {
		return MapError(err)
	}
	fail := func(err error) error {
		out.Close()
		_ = r.r.Remove(osPath(tmp))
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		n, err := io.CopyN(out, in, copyChunk)
		prog.add(0, n)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(MapError(err))
		}
	}
	if err := out.Close(); err != nil {
		_ = r.r.Remove(osPath(tmp))
		return MapError(err)
	}
	_ = r.r.Chtimes(osPath(tmp), time.Time{}, fi.ModTime())
	if err := r.publish(tmp, dst, policy == ConflictOverwrite); err != nil {
		_ = r.r.Remove(osPath(tmp))
		return err
	}
	prog.add(1, 0)
	return nil
}

// RemoveTemps deletes the copy temporaries tagged tag that an interrupted operation left at p:
// os do diretório de p (onde fica o temporário de um arquivo) e, se p é uma pasta, os de toda a
// árvore abaixo dela. Nunca segue symlink e nunca apaga nada além de nomes com o prefixo, então
// rodar sobre um destino que terminou inteiro não tira nada do lugar.
func (r *Root) RemoveTemps(ctx context.Context, p, tag string) (int, error) {
	prefix := CopyTempPrefix(tag)
	n, err := r.removeTempsIn(ctx, Dir(p), prefix, Depth(Dir(p)), false)
	if err != nil || p == "" {
		return n, err
	}
	fi, err := r.r.Lstat(osPath(p))
	if err != nil || !fi.IsDir() {
		return n, nil
	}
	m, err := r.removeTempsIn(ctx, p, prefix, Depth(p), true)
	return n + m, err
}

func (r *Root) removeTempsIn(ctx context.Context, dir, prefix string, depth int, recurse bool) (int, error) {
	f, err := r.r.Open(osPath(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, MapError(err)
	}
	des, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return 0, MapError(err)
	}
	n := 0
	for _, de := range des {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		name := de.Name()
		switch {
		case strings.HasPrefix(name, prefix) && de.Type().IsRegular():
			if err := r.r.Remove(osPath(Join(dir, name))); err == nil {
				n++
			}
		case recurse && de.IsDir() && !strings.HasPrefix(name, ReservedPrefix) && depth+1 < WalkMaxDepth:
			m, err := r.removeTempsIn(ctx, Join(dir, name), prefix, depth+1, true)
			n += m
			if err != nil {
				return n, err
			}
		}
	}
	return n, nil
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
