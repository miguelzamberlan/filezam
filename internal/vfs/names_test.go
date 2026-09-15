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
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNameKey(t *testing.T) {
	same := [][2]string{
		{"C8347.MP4", "c8347.mp4"},
		{"Ação.txt", "ação.TXT"}, // NFD (macOS) × NFC
		{"ΟΔΥΣΣΕΥΣ", "οδυσσευς"},                              // sigma final
		{"Relatório", "RELATÓRIO"},
	}
	for _, p := range same {
		if NameKey(p[0]) != NameKey(p[1]) {
			t.Errorf("%q and %q should be the same name", p[0], p[1])
		}
	}
	for _, p := range [][2]string{{"relatório.pdf", "relatorio.pdf"}, {"a.txt", "a.txt "}, {"foto1.jpg", "foto01.jpg"}} {
		if NameKey(p[0]) == NameKey(p[1]) {
			t.Errorf("%q and %q must stay different", p[0], p[1])
		}
	}
}

func lsNames(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, de := range des {
		out = append(out, de.Name())
	}
	sort.Strings(out)
	return out
}

// Nenhuma operação que cria nome deixa dois itens equivalentes lado a lado.
func TestEquivalentNamesAreTaken(t *testing.T) {
	r, root, outside := fixture(t)

	// Finalize sem overwrite: conflito, com o nome que está lá
	part := func(id, body string) {
		t.Helper()
		f, err := r.CreatePart("a", id, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(body)
		f.Close()
	}
	part("p1", "novo")
	_, err := r.Finalize("a", "p1", "FILE.TXT", false, nil)
	var taken *NameTakenError
	if !errors.As(err, &taken) || !errors.Is(err, ErrExists) || taken.Existing != "file.txt" {
		t.Fatalf("finalize onto an equivalent name: %v", err)
	}
	// com overwrite grava sobre o existente e mantém o nome dele
	if name, err := r.Finalize("a", "p1", "FILE.TXT", true, nil); err != nil || name != "file.txt" {
		t.Fatalf("overwrite equivalent: %q %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "file.txt")); string(b) != "novo" {
		t.Fatalf("overwrite content: %q", b)
	}
	if got := lsNames(t, filepath.Join(root, "a")); len(got) != 2 || got[0] != "file.txt" || got[1] != "sub" {
		t.Fatalf("folder after overwrite: %v", got)
	}

	// Mkdir recusa, MkdirAll reaproveita a pasta existente
	if err := r.Mkdir("A"); !errors.Is(err, ErrExists) {
		t.Fatalf("mkdir equivalent: %v", err)
	}
	if p, err := r.MkdirAll("A/SUB/nova"); err != nil || p != "a/sub/nova" {
		t.Fatalf("mkdirall reuses folders: %q %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(root, "A")); !os.IsNotExist(err) {
		t.Fatal("MkdirAll created a second folder")
	}
	// segmento equivalente a um arquivo é conflito, não pasta nova ao lado
	if _, err := r.MkdirAll("a/FILE.txt/x"); !errors.Is(err, ErrExists) {
		t.Fatalf("mkdirall over a file: %v", err)
	}

	// Rename: só a caixa do próprio nome pode mudar
	if _, err := r.Rename("a/sub/nova", "SUB"); err != nil {
		t.Fatal(err) // outra pasta, sem equivalente
	}
	if _, err := r.Rename("a/sub/SUB", "sub"); err != nil {
		t.Fatalf("case-only rename of itself: %v", err)
	}
	if _, err := r.Rename("a/sub", "File.txt"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto an equivalent name: %v", err)
	}

	// nomes livres e destino de cópia contam equivalentes
	os.WriteFile(filepath.Join(root, "a", "File (1).TXT"), []byte("x"), 0o644)
	if u, err := r.UniqueName("a", "file.txt"); err != nil || u != "file (2).txt" {
		t.Fatalf("unique name: %q %v", u, err)
	}
	if u, err := r.UniqueIfExists("a", "FILE.txt"); err != nil || u != "FILE (2).txt" {
		t.Fatalf("unique if exists: %q %v", u, err)
	}
	os.WriteFile(filepath.Join(root, "outro.TXT"), []byte("x"), 0o644)
	if dst, ok, err := r.ResolveDest("outro.TXT", "a", ConflictOverwrite); err != nil || !ok || dst != "a/outro.TXT" {
		t.Fatalf("resolve free: %q %v %v", dst, ok, err)
	}
	os.WriteFile(filepath.Join(root, "FILE.txt"), []byte("de fora"), 0o644)
	if dst, ok, err := r.ResolveDest("FILE.txt", "a", ConflictOverwrite); err != nil || !ok || dst != "a/file.txt" {
		t.Fatalf("resolve overwrite onto equivalent: %q %v %v", dst, ok, err)
	}
	if dst, ok, err := r.ResolveDest("FILE.txt", "a", ConflictSkip); err != nil || ok || dst != "a/file.txt" {
		t.Fatalf("resolve skip: %q %v %v", dst, ok, err)
	}
	if dst, ok, err := r.ResolveDest("FILE.txt", "a", ConflictRename); err != nil || !ok || dst != "a/FILE (2).txt" {
		t.Fatalf("resolve rename: %q %v %v", dst, ok, err)
	}

	// MoveTo com overwrite substitui o equivalente e fica com o nome dele
	if dst, err := r.MoveTo("FILE.txt", "a/FILE.txt", false); !errors.Is(err, ErrExists) {
		t.Fatalf("move onto equivalent: %q %v", dst, err)
	}
	if dst, err := r.MoveTo("FILE.txt", "a/FILE.txt", true); err != nil || dst != "a/file.txt" {
		t.Fatalf("move overwrite: %q %v", dst, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "file.txt")); string(b) != "de fora" {
		t.Fatalf("moved content: %q", b)
	}

	// cópia de árvore junta pastas equivalentes em vez de criar outra
	os.MkdirAll(filepath.Join(root, "src", "SUB"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "SUB", "novo.txt"), []byte("n"), 0o644)
	os.WriteFile(filepath.Join(root, "src", "FILE.TXT"), []byte("c"), 0o644)
	if err := r.CopyTree(context.Background(), "src", "a", ConflictSkip, &Progress{}); err != nil {
		t.Fatal(err)
	}
	if got := lsNames(t, filepath.Join(root, "a")); len(got) != 3 || got[0] != "File (1).TXT" || got[1] != "file.txt" || got[2] != "sub" {
		t.Fatalf("copy merged into equivalents: %v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "sub", "novo.txt")); err != nil {
		t.Fatalf("copy into the existing folder: %v", err)
	}

	// Lookup não enxerga nada fora da raiz
	for _, dir := range []string{"link-out", "link-etc", ".."} {
		if cur, err := r.Lookup(dir, "CANARY.TXT"); err == nil || cur != "" {
			t.Errorf("Lookup(%q) escaped: %q %v", dir, cur, err)
		}
	}
	if _, err := r.MkdirAll("LINK-OUT/x"); err == nil {
		t.Error("MkdirAll through an escaping link (by equivalent name) succeeded")
	}
	checkCanary(t, outside)
}

func TestExtractMergesEquivalentNames(t *testing.T) {
	r, root, _ := fixture(t)
	makeZip(t, root, "caixa.zip", []zipEntry{
		{name: "Docs/a.txt", body: "a"},
		{name: "docs/b.txt", body: "b"},
		{name: "Leia.TXT", body: "primeiro"},
		{name: "leia.txt", body: "segundo"},
	})
	os.Mkdir(filepath.Join(root, "saida"), 0o755)
	res, err := r.ExtractZip(context.Background(), "caixa.zip", "saida", bigLimits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 || res.Skipped != 1 {
		t.Fatalf("result: %+v", res)
	}
	if got := lsNames(t, filepath.Join(root, "saida")); len(got) != 2 || got[0] != "Docs" || got[1] != "Leia.TXT" {
		t.Fatalf("extracted: %v", got)
	}
	if got := lsNames(t, filepath.Join(root, "saida", "Docs")); len(got) != 2 {
		t.Fatalf("merged folder: %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "saida", "Leia.TXT")); string(b) != "primeiro" {
		t.Fatalf("the second equivalent must not overwrite the first: %q", b)
	}
}
