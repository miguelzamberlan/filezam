package vfs

import (
	"archive/zip"
	"context"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// ZipPartSize é o tamanho máximo de cada parte quando um download em zip é dividido. Cada parte
// é um .zip completo, que abre sozinho: é o que o Google Drive faz com pastas grandes, e evita
// que um download de dezenas de gigabytes dependa de uma única conexão ficar de pé até o fim.
const ZipPartSize = int64(2) << 30

// ZipPart is one piece of a split download: the entries whose names fall in [From, To) in zip
// order ("" = open end), with the files and bytes they carry.
type ZipPart struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// ZipOrder compares two entry names in the order WriteZip writes them: pasta por pasta, com os
// nomes de cada pasta em ordem de bytes. Não é a comparação de strings ("a b" viria antes de
// "a/x", mas a pasta "a" e o que está dentro dela são escritos antes da irmã "a b").
func ZipOrder(a, b string) int {
	for {
		sa, ra, moreA := strings.Cut(a, "/")
		sb, rb, moreB := strings.Cut(b, "/")
		if c := strings.Compare(sa, sb); c != 0 {
			return c
		}
		switch {
		case !moreA && !moreB:
			return 0
		case !moreA:
			return -1
		case !moreB:
			return 1
		}
		a, b = ra, rb
	}
}

// zipWalk visits every entry a zip of paths holds, in ZipOrder, with its name inside the zip.
func (r *Root) zipWalk(ctx context.Context, paths []string, fn func(name, path string, fi fs.FileInfo) error) error {
	sorted := append([]string(nil), paths...)
	slices.SortFunc(sorted, func(a, b string) int { return ZipOrder(Base(a), Base(b)) })
	for _, p := range sorted {
		base := Base(p)
		if p == "" {
			base = ""
		}
		err := r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
			rel := strings.TrimPrefix(strings.TrimPrefix(path, p), "/")
			name := Join(base, rel)
			if name == "" || strings.HasPrefix(fi.Name(), ReservedPrefix) {
				return nil
			}
			if !fi.IsDir() && !fi.Mode().IsRegular() {
				return nil
			}
			return fn(name, path, fi)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// PlanZipParts splits a zip of paths into parts of at most partSize bytes of file content. Um
// arquivo maior que partSize vai sozinho numa parte. As fronteiras são nomes, não posições: se
// algo mudar na pasta entre o plano e o download de uma parte, nenhum arquivo aparece em duas.
func (r *Root) PlanZipParts(ctx context.Context, paths []string, partSize int64) ([]ZipPart, error) {
	parts := []ZipPart{{}}
	err := r.zipWalk(ctx, paths, func(name, _ string, fi fs.FileInfo) error {
		if fi.IsDir() {
			return nil
		}
		cur := &parts[len(parts)-1]
		if cur.Files > 0 && cur.Bytes+fi.Size() > partSize {
			cur.To = name
			parts = append(parts, ZipPart{From: name})
			cur = &parts[len(parts)-1]
		}
		cur.Files++
		cur.Bytes += fi.Size()
		return nil
	})
	return parts, err
}

// WriteZip streams a Store-method zip of the given paths to w.
// Directories are recursed; symlinks and special files are skipped.
func (r *Root) WriteZip(ctx context.Context, w io.Writer, paths []string) error {
	return r.WriteZipPart(ctx, w, paths, ZipPart{})
}

// WriteZipPart streams only the entries of part (names in [part.From, part.To)).
func (r *Root) WriteZipPart(ctx context.Context, w io.Writer, paths []string, part ZipPart) error {
	zw := zip.NewWriter(w)
	err := r.zipWalk(ctx, paths, func(name, path string, fi fs.FileInfo) error {
		if (part.From != "" && ZipOrder(name, part.From) < 0) || (part.To != "" && ZipOrder(name, part.To) >= 0) {
			return nil
		}
		if fi.IsDir() {
			hdr := &zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: fi.ModTime()}
			hdr.SetMode(fi.Mode())
			_, err := zw.CreateHeader(hdr)
			return err
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
