package vfs

import (
	"archive/zip"
	"context"
	"io"
	"io/fs"
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
