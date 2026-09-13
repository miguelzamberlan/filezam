// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package thumbs

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSupported(t *testing.T) {
	for _, n := range []string{"a.jpg", "A.JPEG", "b.png", "c.gif", "d.webp", "e.bmp", "f.tiff"} {
		if !Supported(n) {
			t.Errorf("Supported(%q) = false", n)
		}
	}
	// Sem decodificador na imagem estática: caem no ícone, não em erro.
	for _, n := range []string{"a.heic", "b.avif", "c.mp4", "d.pdf", "e.txt", "f.cr2", "noext"} {
		if Supported(n) {
			t.Errorf("Supported(%q) = true", n)
		}
	}
}

// Prune apaga os mais antigos até caber no teto — e não encosta em nada quando já cabe.
func TestPrune(t *testing.T) {
	dir := t.TempDir()
	c, err := New(filepath.Join(dir, "thumbs"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, size int, age time.Duration) string {
		p := filepath.Join(c.dir, name[:2], name+".jpg")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o640); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-age)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	velho := write("aa1", 1000, 3*time.Hour)
	medio := write("bb2", 1000, 2*time.Hour)
	novo := write("cc3", 1000, time.Hour)

	// Cabe: nada é apagado.
	c.Prune(10000)
	for _, p := range []string{velho, medio, novo} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("pruned while under the cap: %s", p)
		}
	}
	// Não cabe: o mais antigo sai primeiro, e para assim que couber.
	c.Prune(2500)
	if _, err := os.Stat(velho); !os.IsNotExist(err) {
		t.Fatal("oldest entry survived")
	}
	for _, p := range []string{medio, novo} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("pruned more than needed: %s", p)
		}
	}
	// Teto zerado (desligado) não apaga nada.
	c.Prune(0)
	if _, err := os.Stat(novo); err != nil {
		t.Fatal("pruned with no cap set")
	}
}
