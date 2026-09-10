package index

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

func newIndexer(t *testing.T) (*Indexer, *store.DB, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "data")
	for _, p := range []string{"docs/sub", "fotos", ".filezam-trash/x"} {
		os.MkdirAll(filepath.Join(root, p), 0o755)
	}
	for _, p := range []string{"docs/relatorio.pdf", "docs/sub/Relatorio-final.txt", "fotos/praia.jpg", ".filezam-trash/x/relatorio-apagado.pdf", "docs/.filezam-upload-ab.part"} {
		os.WriteFile(filepath.Join(root, p), []byte("x"), 0o644)
	}
	db, err := store.Open(filepath.Join(dir, "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	base, err := vfs.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	return New(db, base, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour), db, root
}

func search(t *testing.T, ix *Indexer, prefix, q string) []string {
	t.Helper()
	rows, _, err := ix.Search(context.Background(), prefix, q, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.Path)
	}
	sort.Strings(out)
	return out
}

func eq(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFullScanAndSearch(t *testing.T) {
	ix, _, _ := newIndexer(t)
	if ix.Ready() {
		t.Fatal("ready before the first scan")
	}
	ctx := context.Background()
	if ran, err := ix.FullScan(ctx); !ran || err != nil {
		t.Fatal(ran, err)
	}
	st := ix.Status(ctx)
	// docs, docs/sub, fotos + 3 arquivos; lixeira e partes de upload nunca entram
	if !st.Ready || st.Entries != 6 || st.LastFullAt == nil {
		t.Fatalf("status: %+v", st)
	}
	// nome: substring sem diferenciar maiúsculas; prefixo do caminho: exato (LIKE sensível)
	if got := search(t, ix, "", "RELATORIO"); !eq(got, "docs/relatorio.pdf", "docs/sub/Relatorio-final.txt") {
		t.Fatalf("search: %v", got)
	}
	if got := search(t, ix, "fotos", "a"); !eq(got, "fotos/praia.jpg") {
		t.Fatalf("prefix search: %v", got)
	}
	if got := search(t, ix, "Fotos", "a"); len(got) != 0 {
		t.Fatalf("prefix matched another case: %v", got)
	}
	if got := search(t, ix, "", "%"); len(got) != 0 {
		t.Fatalf("%% is not literal: %v", got)
	}
	if got := search(t, ix, "", "apagado"); len(got) != 0 {
		t.Fatalf("trash indexed: %v", got)
	}
}

// Linhas velhas: o que sumiu do disco sai do índice na próxima varredura completa (geração
// nova), ao reindexar a subárvore ou num Touch do próprio caminho.
func TestStaleRowsAreDropped(t *testing.T) {
	ix, db, root := newIndexer(t)
	ctx := context.Background()
	ix.FullScan(ctx)

	os.Remove(filepath.Join(root, "fotos", "praia.jpg"))
	os.WriteFile(filepath.Join(root, "fotos", "serra.jpg"), []byte("x"), 0o644)
	if got := search(t, ix, "", "praia"); len(got) != 1 {
		t.Fatalf("external change seen before a scan: %v", got) // limitação documentada
	}
	ix.FullScan(ctx)
	if got := search(t, ix, "", ".jpg"); !eq(got, "fotos/serra.jpg") {
		t.Fatalf("after full scan: %v", got)
	}

	// ReindexTree troca a subárvore inteira
	os.RemoveAll(filepath.Join(root, "docs", "sub"))
	os.WriteFile(filepath.Join(root, "docs", "novo.txt"), []byte("x"), 0o644)
	if err := ix.ReindexTree(ctx, "docs"); err != nil {
		t.Fatal(err)
	}
	if got := search(t, ix, "docs", ""); !eq(got, "docs/novo.txt", "docs/relatorio.pdf") {
		t.Fatalf("after reindex: %v", got)
	}

	// Touch de um caminho que não existe mais apaga a linha (e o que houver abaixo)
	os.RemoveAll(filepath.Join(root, "fotos"))
	if err := ix.Touch(ctx, "fotos"); err != nil {
		t.Fatal(err)
	}
	if got := search(t, ix, "", "serra"); len(got) != 0 {
		t.Fatalf("touch kept a missing path: %v", got)
	}
	if n, _ := db.IndexCount(ctx); n != 3 { // docs, docs/novo.txt, docs/relatorio.pdf
		t.Fatalf("entries: %d", n)
	}

	// RemoveTree não confunde prefixo de nome com subpasta
	os.MkdirAll(filepath.Join(root, "docs2"), 0o755)
	ix.Touch(ctx, "docs2")
	ix.RemoveTree(ctx, "docs")
	if got := search(t, ix, "", "docs"); !eq(got, "docs2") {
		t.Fatalf("remove tree: %v", got)
	}
}

func TestOneFullScanAtATime(t *testing.T) {
	ix, _, _ := newIndexer(t)
	ix.scanMu.Lock()
	if ran, err := ix.FullScan(context.Background()); ran || err != nil {
		t.Fatalf("second scan ran concurrently: %v %v", ran, err)
	}
	ix.scanMu.Unlock()
}
