package vfs

import (
	"archive/zip"
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
)

// WriteZip streams a Store-method zip of the given paths to w.
// Directories are recursed; symlinks and special files are skipped.
func (r *Root) WriteZip(ctx context.Context, w io.Writer, paths []string) error {
	zw := zip.NewWriter(w)
	for _, p := range paths {
		base := Base(p)
		if p == "" {
			base = ""
		}
		err := r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
			rel := strings.TrimPrefix(path, p)
			rel = strings.TrimPrefix(rel, "/")
			name := Join(base, rel)
			if fi.IsDir() {
				if name == "" {
					return nil
				}
				hdr := &zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: fi.ModTime()}
				hdr.SetMode(fi.Mode())
				_, err := zw.CreateHeader(hdr)
				return err
			}
			if !fi.Mode().IsRegular() || strings.HasPrefix(fi.Name(), ReservedPrefix) {
				return nil
			}
			hdr := &zip.FileHeader{Name: name, Method: zip.Store, Modified: fi.ModTime()}
			hdr.SetMode(fi.Mode())
			hdr.UncompressedSize64 = uint64(fi.Size())
			dst, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := r.r.Open(path)
			if err != nil {
				return nil // vanished; skip
			}
			defer f.Close()
			_, err = io.Copy(dst, f)
			return err
		})
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// UniqueIfExists returns name, or the next free "name (n).ext" when it is taken.
func (r *Root) UniqueIfExists(dir, name string) (string, error) {
	ok, err := r.Exists(Join(dir, name))
	if err != nil || !ok {
		return name, err
	}
	return r.UniqueName(dir, name)
}

// ZipTempName is where WriteZipFile assembles name before publishing it.
func ZipTempName(name string) string { return ReservedPrefix + "zip-" + name }

// WriteZipFile writes a zip of paths into dir/name. Diferente do WriteZip, que transmite direto
// para a resposta HTTP, aqui o destino é o próprio disco: grava num arquivo temporário e só
// publica no fim, para uma compactação interrompida não deixar um .zip pela metade com nome
// legítimo.
func (r *Root) WriteZipFile(ctx context.Context, dir, name string, paths []string, prog *Progress) error {
	if err := ValidName(name); err != nil {
		return err
	}
	tmp := ZipTempName(name)
	f, err := r.r.OpenFile(Join(dir, tmp), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return MapError(err)
	}
	clean := func() { f.Close(); _ = r.r.Remove(Join(dir, tmp)) }
	if err := r.WriteZip(ctx, &progressWriter{w: f, prog: prog}, paths); err != nil {
		clean()
		return err
	}
	if err := f.Close(); err != nil {
		_ = r.r.Remove(Join(dir, tmp))
		return MapError(err)
	}
	if err := r.r.Rename(Join(dir, tmp), Join(dir, name)); err != nil {
		_ = r.r.Remove(Join(dir, tmp))
		return MapError(err)
	}
	return nil
}

// progressWriter reporta bytes gravados enquanto o zip é montado.
type progressWriter struct {
	w    io.Writer
	prog *Progress
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.prog.add(0, int64(n))
	return n, err
}
