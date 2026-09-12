// Package thumbs generates and caches image thumbnails on disk.
package thumbs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "image/gif" // decodificadores registrados por efeito colateral
	_ "image/png"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Size é o lado maior da miniatura, em pixels. Um valor só, fixo: a grade usa uma caixa de 56 px
// e a lista 32, então 256 cobre os dois inclusive em tela de alta densidade — e um tamanho
// arbitrário vindo da URL transformaria o cache num balde sem fundo.
const Size = 256

// cacheVersion entra na chave: mudar o algoritmo ou o tamanho invalida o que já foi gerado.
const cacheVersion = 1

// ErrUnsupported: o arquivo não é uma imagem que a gente saiba decodificar.
var ErrUnsupported = errors.New("unsupported image")

// ErrTooLarge: a imagem passa dos limites e não será nem decodificada.
var ErrTooLarge = errors.New("image too large")

// Ext lista as extensões com decodificador. HEIC, AVIF e RAW exigiriam CGO, que a imagem
// distroless não tem; vídeo exigiria ffmpeg. Esses caem no ícone normal.
var ext = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".bmp": true, ".tif": true, ".tiff": true,
}

// Supported reports whether a name looks like an image we can thumbnail.
func Supported(name string) bool { return ext[strings.ToLower(filepath.Ext(name))] }

// Limits caps what may be decoded, to keep a hostile image from eating the server.
type Limits struct {
	MaxPixels int64 // largura × altura, lido do cabeçalho antes de decodificar
	MaxFile   int64 // tamanho do arquivo em bytes
}

// Cache stores generated thumbnails under dir (inside DataDir, never in the user's tree).
type Cache struct {
	dir string
	log *slog.Logger

	mu      sync.Mutex
	running map[string]chan struct{} // evita gerar a mesma miniatura duas vezes em paralelo
}

// New prepares the cache directory.
func New(dir string, log *slog.Logger) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Cache{dir: dir, log: log, running: map[string]chan struct{}{}}, nil
}

// key identifies one thumbnail. Inclui dev/ino para que o mesmo arquivo alcançado por dois
// caminhos use uma entrada só, e mtime/size para que qualquer alteração gere uma chave nova —
// é o que torna a invalidação automática.
func key(dev, ino uint64, mtime, size int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%d|%d|%d|%d", cacheVersion, dev, ino, mtime, size, Size)
	return hex.EncodeToString(h.Sum(nil))
}

// path espalha o cache em 256 subpastas: um diretório único com centenas de milhares de arquivos
// fica lento para listar e para limpar.
func (c *Cache) path(k string) string { return filepath.Join(c.dir, k[:2], k+".jpg") }

// Get returns the path of the thumbnail for p, generating it if needed.
func (c *Cache) Get(ctx context.Context, root *vfs.Root, p string, mtime, size int64, lim Limits) (string, error) {
	if !Supported(p) {
		return "", ErrUnsupported
	}
	if lim.MaxFile > 0 && size > lim.MaxFile {
		return "", ErrTooLarge
	}
	dev, ino, err := root.Identity(p)
	if err != nil {
		return "", err
	}
	k := key(dev, ino, mtime, size)
	dst := c.path(k)
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	// Duas requisições para a mesma imagem (rolar a lista para cima e para baixo) não decodificam
	// duas vezes: a segunda espera a primeira terminar.
	c.mu.Lock()
	wait, busy := c.running[k]
	if busy {
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if _, err := os.Stat(dst); err == nil {
			return dst, nil
		}
		return "", ErrUnsupported
	}
	done := make(chan struct{})
	c.running[k] = done
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.running, k)
		c.mu.Unlock()
		close(done)
	}()

	if err := c.generate(root, p, dst, lim); err != nil {
		return "", err
	}
	return dst, nil
}

// generate decodes the image and writes the thumbnail.
func (c *Cache) generate(root *vfs.Root, p, dst string, lim Limits) error {
	f, _, err := root.OpenFile(p)
	if err != nil {
		return err
	}
	defer f.Close()
	// DecodeConfig lê só o cabeçalho: é o que impede uma imagem declarada como 50000×50000 de
	// alocar gigabytes antes de qualquer redimensionamento.
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ErrUnsupported
	}
	if lim.MaxPixels > 0 && int64(cfg.Width)*int64(cfg.Height) > lim.MaxPixels {
		return ErrTooLarge
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return ErrUnsupported
	}
	out := resize(src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 75}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	// Grava num temporário e renomeia: uma geração interrompida não deixa um JPEG truncado no
	// cache, que seria servido como se estivesse bom.
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// resize scales src down so its longest side is Size, flattening transparency onto white (a
// thumbnail in JPEG has no alpha, and a black background would look broken).
func resize(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > h {
		if w > Size {
			h = h * Size / w
			w = Size
		}
	} else if h > Size {
		w = w * Size / h
		h = Size
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

// Prune keeps the cache under max bytes, dropping the least recently generated first.
func (c *Cache) Prune(max int64) {
	if max <= 0 {
		return
	}
	type ent struct {
		path string
		size int64
		mod  time.Time
	}
	var all []ent
	var total int64
	err := filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		all = append(all, ent{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
		return nil
	})
	if err != nil || total <= max {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, e := range all {
		if total <= max {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
	c.log.Info("thumbnail cache pruned", "bytes", total, "max", max)
}

// Clear drops the whole cache (used when the feature is turned off).
func (c *Cache) Clear() error { return os.RemoveAll(c.dir) }

// ETag identifies a cached thumbnail. O nome do arquivo de cache é o próprio hash da chave, que
// muda com o conteúdo, então ele já serve de validador.
func ETag(p string) string {
	return `"` + strings.TrimSuffix(filepath.Base(p), ".jpg") + `"`
}
