package vfs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fixture creates: outside/canary.txt, root/{a/file.txt, a/sub/deep.txt, link-etc -> /etc,
// link-out -> ../outside, link-in -> a, loop -> loop, ..x/}
func fixture(t *testing.T) (*Root, string, string) {
	t.Helper()
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	root := filepath.Join(base, "root")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "a", "sub"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "..x"), 0o755))
	must(os.MkdirAll(outside, 0o755))
	must(os.WriteFile(filepath.Join(outside, "canary.txt"), []byte("canary"), 0o644))
	must(os.WriteFile(filepath.Join(root, "a", "file.txt"), []byte("hello"), 0o644))
	must(os.WriteFile(filepath.Join(root, "a", "sub", "deep.txt"), []byte("deep"), 0o644))
	must(os.Symlink("/etc", filepath.Join(root, "link-etc")))
	must(os.Symlink("../outside", filepath.Join(root, "link-out")))
	must(os.Symlink("a", filepath.Join(root, "link-in")))
	must(os.Symlink("loop", filepath.Join(root, "loop")))
	r, err := Open(root)
	must(err)
	t.Cleanup(func() { r.Close() })
	return r, root, outside
}

func checkCanary(t *testing.T, outside string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(outside, "canary.txt"))
	if err != nil || string(b) != "canary" {
		t.Fatalf("canary modified or missing: %v %q", err, b)
	}
}

func TestListClassifiesSymlinks(t *testing.T) {
	r, _, _ := fixture(t)
	l, err := r.List("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range l.Entries {
		got[e.Name] = e
	}
	if got["a"].Type != "dir" || got["..x"].Type != "dir" {
		t.Errorf("dirs: %+v", got)
	}
	if e := got["link-in"]; e.Type != "dir" || !e.Link {
		t.Errorf("link-in should be dir link: %+v", e)
	}
	for _, n := range []string{"link-etc", "link-out", "loop"} {
		if e := got[n]; e.Type != "other" || !e.Link {
			t.Errorf("%s should be other: %+v", n, e)
		}
	}
}

func TestEscapeAttemptsAreRefused(t *testing.T) {
	r, _, outside := fixture(t)
	for _, p := range []string{"link-etc", "link-etc/passwd", "link-out", "link-out/canary.txt", "loop"} {
		if _, err := r.List(p); err == nil {
			t.Errorf("List(%q) succeeded", p)
		}
		if _, _, err := r.OpenFile(p); err == nil {
			t.Errorf("OpenFile(%q) succeeded", p)
		}
	}
	if _, err := r.Sub("link-out"); err == nil {
		t.Error("Sub(link-out) succeeded")
	}
	if _, err := r.StatReserved("..", "canary.txt"); err == nil {
		t.Error("StatReserved(../canary.txt) succeeded")
	}
	if _, err := r.StatReserved("link-out", "canary.txt"); err == nil {
		t.Error("StatReserved through escaping link succeeded")
	}
	if err := r.Mkdir("link-out/newdir"); err == nil {
		t.Error("Mkdir through escaping link succeeded")
	}
	if err := r.CopyTree(context.Background(), "a", "link-out/copy", ConflictRename, nil); err == nil {
		t.Error("CopyTree into escaping link succeeded")
	}
	if _, err := r.Move("a", "link-out", false); err == nil {
		t.Error("Move into escaping link succeeded")
	}
	// Removing the link removes only the link.
	if err := r.Remove("link-out"); err != nil {
		t.Fatal(err)
	}
	checkCanary(t, outside)
	if _, err := os.Lstat(filepath.Join(outside, "canary.txt")); err != nil {
		t.Fatal("canary deleted")
	}
	// Zip must not include anything outside.
	var buf bytes.Buffer
	if err := r.WriteZip(context.Background(), &buf, []string{""}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("canary")) {
		t.Error("zip contains outside data")
	}
}

func TestInsideSymlinkWorks(t *testing.T) {
	r, _, _ := fixture(t)
	l, err := r.List("link-in")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 {
		t.Errorf("expected 2 entries via link-in, got %d", len(l.Entries))
	}
	f, fi, err := r.OpenFile("link-in/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if fi.Size() != 5 {
		t.Error("size")
	}
}

func TestScopeRoot(t *testing.T) {
	r, _, _ := fixture(t)
	s, err := r.Sub("a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 {
		t.Errorf("scope list: %d", len(l.Entries))
	}
	if _, err := s.List("../.."); err == nil {
		t.Error("scope escape via .. succeeded")
	}
	if _, err := s.Stat("..x"); err == nil {
		t.Error("sibling reachable from scope")
	}
	if ok, _ := s.Exists("sub/deep.txt"); !ok {
		t.Error("nested file missing")
	}
}

func TestRenameMoveCopyRemove(t *testing.T) {
	r, root, _ := fixture(t)
	ctx := context.Background()
	if _, err := r.Rename("", "x"); !errors.Is(err, ErrRootOp) {
		t.Error("rename root allowed")
	}
	if err := r.Remove(""); !errors.Is(err, ErrRootOp) {
		t.Error("remove root allowed")
	}
	if _, err := r.Rename("a/file.txt", "sub"); !errors.Is(err, ErrExists) {
		t.Errorf("rename over existing: %v", err)
	}
	if _, err := r.Rename("a/file.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("b"); !errors.Is(err, ErrExists) {
		t.Errorf("mkdir exists: %v", err)
	}
	if _, err := r.Move("a", "a/sub", false); !errors.Is(err, ErrNested) {
		t.Errorf("nested move: %v", err)
	}
	if err := r.CopyTree(ctx, "a", "a/sub/copy", ConflictRename, nil); !errors.Is(err, ErrNested) {
		t.Errorf("nested copy: %v", err)
	}
	var files int
	var bs int64
	prog := &Progress{Add: func(f int, b int64) { files += f; bs += b }}
	if err := r.CopyTree(ctx, "a", "b/a", ConflictRename, prog); err != nil {
		t.Fatal(err)
	}
	if files != 2 || bs != 9 {
		t.Errorf("progress files=%d bytes=%d", files, bs)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "b", "a", "sub", "deep.txt")); string(b) != "deep" {
		t.Error("copied content")
	}
	// mtime preserved
	src, _ := os.Stat(filepath.Join(root, "a", "renamed.txt"))
	dst, _ := os.Stat(filepath.Join(root, "b", "a", "renamed.txt"))
	if !src.ModTime().Equal(dst.ModTime()) {
		t.Error("mtime not preserved")
	}
	// conflict: rename
	dst2, ok, err := r.ResolveDest("a/renamed.txt", "b/a", ConflictRename)
	if err != nil || !ok || dst2 != "b/a/renamed (1).txt" {
		t.Errorf("ResolveDest rename: %q %v %v", dst2, ok, err)
	}
	if _, ok, _ := r.ResolveDest("a/renamed.txt", "b/a", ConflictSkip); ok {
		t.Error("skip should not be ok")
	}
	// move
	if _, err := r.Move("b/a/sub", "a", false); !errors.Is(err, ErrExists) {
		t.Errorf("move onto existing: %v", err)
	}
	if _, err := r.Move("b/a/renamed.txt", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Error("moved file missing")
	}
	// remove tree
	tot, err := r.Scan(ctx, "b")
	if err != nil || tot.Files != 1 {
		t.Errorf("scan: %+v %v", tot, err)
	}
	if err := r.RemoveTree(ctx, "b", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "b")); !os.IsNotExist(err) {
		t.Error("b not removed")
	}
	// cancellation
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := r.CopyTree(cctx, "a", "c", ConflictRename, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
}

func TestUploadPartFinalize(t *testing.T) {
	r, root, _ := fixture(t)
	f, err := r.CreatePart("a", "id1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("0123456789"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := r.CreatePart("a", "id1", 10); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate part: %v", err)
	}
	l, _ := r.List("a")
	if len(l.Parts) != 1 || len(l.Entries) != 2 {
		t.Errorf("part visible? parts=%v entries=%d", l.Parts, len(l.Entries))
	}
	if err := r.Finalize("a", "id1", "file.txt", false); !errors.Is(err, ErrExists) {
		t.Errorf("finalize over existing without overwrite: %v", err)
	}
	if err := r.Finalize("a", "id1", "new.txt", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "new.txt")); string(b) != "0123456789" {
		t.Error("finalized content")
	}
	if _, err := os.Lstat(filepath.Join(root, "a", PartName("id1"))); !os.IsNotExist(err) {
		t.Error("part left behind")
	}
	f, _ = r.CreatePart("a", "id2", 3)
	f.WriteAt([]byte("abc"), 0)
	f.Close()
	if err := r.Finalize("a", "id2", "sub", true); !errors.Is(err, ErrIsDir) {
		t.Errorf("finalize onto dir: %v", err)
	}
	if err := r.Finalize("a", "id2", "file.txt", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "file.txt")); string(b) != "abc" {
		t.Error("overwrite content")
	}
	if err := r.Chtimes("a/file.txt", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := r.RemovePart("a", "missing"); err != nil {
		t.Error("RemovePart missing should be nil")
	}
}

// Find nunca segue symlinks nem sai do root: o canário fora do root e o /etc
// apontados por links dentro do root não aparecem, e os limites param a busca.
func TestFindStaysInsideRoot(t *testing.T) {
	r, root, outside := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "a", "sub", ".filezam-upload-x.part"), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	lim := SearchLimits{MaxScan: 1000, MaxResults: 100}
	hits, partial, err := r.Find(ctx, "", "canary", lim)
	if err != nil || partial || len(hits) != 0 {
		t.Fatalf("canary reachable: %v %v %+v", err, partial, hits)
	}
	hits, _, err = r.Find(ctx, "", "passwd", lim)
	if err != nil || len(hits) != 0 {
		t.Fatalf("/etc reachable through link: %v %+v", err, hits)
	}
	hits, partial, err = r.Find(ctx, "", "TXT", lim)
	if err != nil || partial {
		t.Fatal(err, partial)
	}
	got := map[string]string{}
	for _, h := range hits {
		got[Join(h.Dir, h.Entry.Name)] = h.Entry.Type
	}
	if len(got) != 2 || got["a/file.txt"] != "file" || got["a/sub/deep.txt"] != "file" {
		t.Fatalf("hits: %v", got)
	}
	hits, _, _ = r.Find(ctx, "", "filezam", lim)
	if len(hits) != 0 {
		t.Fatalf("reserved part listed: %+v", hits)
	}
	hits, partial, err = r.Find(ctx, "a", "sub", lim)
	if err != nil || partial || len(hits) != 1 || hits[0].Dir != "a" || hits[0].Entry.Type != "dir" {
		t.Fatalf("dir hit under a: %v %v %+v", err, partial, hits)
	}
	if _, partial, err := r.Find(ctx, "", ".", SearchLimits{MaxScan: 2, MaxResults: 100}); err != nil || !partial {
		t.Fatalf("scan limit: %v %v", err, partial)
	}
	if hits, partial, err := r.Find(ctx, "", "txt", SearchLimits{MaxScan: 1000, MaxResults: 1}); err != nil || !partial || len(hits) != 1 {
		t.Fatalf("result limit: %v %v %d", err, partial, len(hits))
	}
	if _, _, err := r.Find(ctx, "", "  ", lim); err == nil {
		t.Fatal("empty query accepted")
	}
	checkCanary(t, outside)
}

// Conteúdo da lixeira (nome reservado) nunca é copiado, zipado, contado nem encontrado.
func TestTrashIsInvisibleToTreeOps(t *testing.T) {
	r, root, outside := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "a", TrashDirName, "id1"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a", TrashDirName, "id1", "secret.txt"), []byte("trash"), 0o644)
	ctx := context.Background()
	if tot, err := r.ScanLimited(ctx, "a", 1000); err != nil || tot.Files != 2 {
		t.Fatalf("scan counted trash: %+v %v", tot, err)
	}
	if err := r.CopyTree(ctx, "a", "acopy", ConflictRename, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "acopy", TrashDirName)); !os.IsNotExist(err) {
		t.Fatal("trash copied")
	}
	var buf bytes.Buffer
	if err := r.WriteZip(ctx, &buf, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("secret.txt")) {
		t.Fatal("trash zipped")
	}
	if hits, _, _ := r.Find(ctx, "", "secret", SearchLimits{MaxScan: 1000, MaxResults: 10}); len(hits) != 0 {
		t.Fatalf("trash found: %+v", hits)
	}
	// mover para a lixeira e restaurar
	if err := r.MoveToTrash(ctx, "a/file.txt", Join("a", TrashDirName), "id2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", TrashDirName, "id2", "file.txt")); err != nil {
		t.Fatal("not in trash")
	}
	if err := r.RestoreFromTrash(ctx, Join("a", TrashDirName), "id2", "file.txt", "a/sub/file.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "sub", "file.txt")); string(b) != "hello" {
		t.Fatalf("restored: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "a", TrashDirName, "id2")); !os.IsNotExist(err) {
		t.Fatal("id dir left behind")
	}
	if err := r.MoveToTrash(ctx, "", TrashDirName, "x"); err == nil {
		t.Fatal("root moved to trash")
	}
	checkCanary(t, outside)
}

func TestWalkEntriesStaysInsideRoot(t *testing.T) {
	r, _, outside := fixture(t)
	seen := map[string]string{}
	if err := r.WalkEntries(context.Background(), "", func(p string, e Entry) error {
		seen[p] = e.Type
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen["a/sub/deep.txt"] != "file" || seen["a"] != "dir" || seen["link-etc"] == "" {
		t.Fatalf("walk: %v", seen)
	}
	for p := range seen {
		if strings.Contains(p, "canary") || strings.Contains(p, "passwd") || strings.HasPrefix(p, "link-etc/") {
			t.Fatalf("escaped: %s", p)
		}
	}
	checkCanary(t, outside)
}

// Walks stop descending at WalkMaxDepth and skip folders they cannot open, so a deep chain
// or one unreadable folder never wedges the index, a search, a quota scan or a zip.
func TestWalkDepthCapAndUnreadableDirs(t *testing.T) {
	r, root, _ := fixture(t)
	chain := "deep"
	for i := 1; i < WalkMaxDepth+20; i++ {
		chain += "/d"
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(chain)), 0o755); err != nil {
		t.Fatal(err)
	}
	maxSeen := 0
	if err := r.WalkEntries(context.Background(), "deep", func(p string, e Entry) error {
		if d := Depth(p); d > maxSeen {
			maxSeen = d
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if maxSeen != WalkMaxDepth {
		t.Fatalf("walk descended to depth %d, want %d", maxSeen, WalkMaxDepth)
	}
	if _, err := Normalize(chain); err == nil {
		t.Fatal("Normalize accepted a path deeper than MaxDepth")
	}

	if os.Geteuid() == 0 {
		t.Log("running as root: permission checks are bypassed, skipping unreadable-dir case")
		return
	}
	os.MkdirAll(filepath.Join(root, "a", "locked", "inner"), 0o755)
	os.WriteFile(filepath.Join(root, "a", "visible.txt"), []byte("x"), 0o644)
	if err := os.Chmod(filepath.Join(root, "a", "locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "a", "locked"), 0o755) })
	seen := map[string]bool{}
	if err := r.WalkEntries(context.Background(), "", func(p string, e Entry) error {
		seen[p] = true
		return nil
	}); err != nil {
		t.Fatalf("one unreadable folder aborted the walk: %v", err)
	}
	if !seen["a/locked"] || !seen["a/visible.txt"] || seen["a/locked/inner"] {
		t.Fatalf("walk: %v", seen)
	}
	if _, err := r.List("a/locked"); err == nil {
		t.Fatal("listing the unreadable folder itself should still fail")
	}
}

// Destino alcançado por um symlink que aponta para dentro da origem (criado fora do app): a
// cópia não desce na pasta que ela mesma criou, então termina em vez de recursar sem fim.
func TestCopyIntoItselfThroughSymlink(t *testing.T) {
	r, root, outside := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var warns []string
	prog := &Progress{Warn: func(p string, err error) { warns = append(warns, p) }}

	// link-in -> a: copiar a para link-in/copy grava em a/copy
	if err := r.CopyTree(ctx, "a", "link-in/copy", ConflictRename, prog); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a/copy/file.txt", "a/copy/sub/deep.txt"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "a", "copy", "copy")); !os.IsNotExist(err) {
		t.Fatalf("copy descended into its own destination: %v", err)
	}
	if len(warns) == 0 {
		t.Fatal("skipped destination not reported")
	}

	// destino mais fundo: x/y -> ../a/sub, cópia de a para x/y/c2 grava em a/sub/c2
	if err := os.MkdirAll(filepath.Join(root, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../a/sub", filepath.Join(root, "x", "y")); err != nil {
		t.Fatal(err)
	}
	if err := r.CopyTree(ctx, "a", "x/y/c2", ConflictRename, prog); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "sub", "c2", "sub", "deep.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "sub", "c2", "sub", "c2")); !os.IsNotExist(err) {
		t.Fatalf("deep copy descended into its own destination: %v", err)
	}
	checkCanary(t, outside)
}

// IsEmpty é a porta de entrada do link de envio: ela decide se uma pasta pode receber
// arquivos de anônimos. Nunca pode ser enganada para fora do root nem fingir que uma pasta
// com parte de upload está vazia.
func TestIsEmptyRefusesEscape(t *testing.T) {
	r, root, outside := fixture(t)
	for _, p := range []string{"link-out", "link-etc", "loop", "link-out/.."} {
		if _, err := r.IsEmpty(p); err == nil {
			t.Errorf("IsEmpty(%q) succeeded", p)
		}
	}
	if _, err := r.IsEmpty(".."); err == nil {
		t.Error(`IsEmpty("..") succeeded`)
	}
	// Pasta realmente vazia x pasta com conteúdo.
	if err := r.Mkdir("empty"); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.IsEmpty("empty"); err != nil || !ok {
		t.Fatalf("empty dir: %v %v", ok, err)
	}
	if ok, err := r.IsEmpty("a"); err != nil || ok {
		t.Fatalf("dir with files reported empty: %v %v", ok, err)
	}
	// Uma parte de upload conta como conteúdo, mesmo sendo invisível na listagem.
	if err := os.WriteFile(filepath.Join(root, "empty", PartName("x")), []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.IsEmpty("empty"); err != nil || ok {
		t.Fatalf("dir with an upload part reported empty: %v %v", ok, err)
	}
	// Um arquivo não é pasta vazia.
	if _, err := r.IsEmpty("a/file.txt"); err == nil {
		t.Error("IsEmpty on a file succeeded")
	}
	checkCanary(t, outside)
}

// zipEntry descreve uma entrada a ser forjada no teste. O archive/zip não valida nome nenhum na
// escrita, então dá para montar exatamente os arquivos maliciosos que interessam.
type zipEntry struct {
	name    string
	body    string
	mode    fs.FileMode
	raw     bool   // escreve o header verbatim (para forjar tamanho, método e flags)
	flags   uint16 // bit 0 = cifrado
	method  uint16
	declare uint64 // UncompressedSize64 mentido
}

func makeZip(t *testing.T, root, name string, entries []zipEntry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Now()}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		if e.raw {
			hdr.Flags = e.flags
			hdr.Method = e.method
			hdr.UncompressedSize64 = e.declare
			hdr.CompressedSize64 = uint64(len(e.body))
			hdr.CRC32 = crc32.ChecksumIEEE([]byte(e.body))
			w, err := zw.CreateRaw(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		// O writer recusa corpo numa entrada de diretório; o nome é o que interessa no teste.
		if e.body != "" && !strings.HasSuffix(e.name, "/") && e.name != "" && e.name != "/" {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

var bigLimits = ExtractLimits{MaxBytes: 1 << 20, MaxEntries: 1000}

// Um arquivo compactado é conteúdo de terceiros: com os links públicos de recebimento, pode ter
// sido depositado por alguém sem conta. Nenhuma entrada pode escrever fora da pasta de destino,
// criar symlink, carregar setuid ou sobrescrever o que já existe.
func TestExtractRefusesEscape(t *testing.T) {
	r, root, outside := fixture(t)
	ctx := context.Background()
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	makeZip(t, root, "evil.zip", []zipEntry{
		{name: "../canary.txt", body: "invadido"},
		{name: "../../canary.txt", body: "invadido"},
		{name: "/etc/passwd", body: "invadido"},
		{name: "a/../../canary.txt", body: "invadido"},
		{name: "....//canary.txt", body: "invadido"},
		{name: ".filezam-upload-x.part", body: "reservado"},
		{name: "sub/.filezam-trash/x", body: "reservado"},
		{name: "ctrl\x01nome.txt", body: "controle"},
		{name: "", body: "vazio"},
		{name: "/", body: "barra"},
		{name: "link", body: "/etc", mode: 0o777 | fs.ModeSymlink},
		{name: "bom.txt", body: "conteudo legitimo"},
	})
	res, err := r.ExtractZip(ctx, "evil.zip", "saida", bigLimits, nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	// As entradas maliciosas viram aviso; o que sobra é inofensivo. Um nome absoluto perde a
	// barra e vira relativo ao destino ("/etc/passwd" → "saida/etc/passwd"), e "...." é um nome
	// literal, não travessia — os dois ficam contidos, que é o que importa.
	if res.Files == 0 || res.Skipped < 6 {
		t.Fatalf("result: %+v", res)
	}
	checkCanary(t, outside)
	if _, err := os.Lstat(filepath.Join(outside, "canary.txt")); err != nil {
		t.Fatal("canary deleted")
	}
	// Nada apareceu fora da pasta de destino.
	for _, p := range []string{"canary.txt", "passwd", "etc", "link"} {
		if _, err := os.Lstat(filepath.Join(root, p)); err == nil {
			t.Errorf("escaped the destination: %q exists at the scope root", p)
		}
	}
	// E dentro dela, nenhum symlink, nenhum nome reservado, e todo caminho continua endereçável.
	saida := filepath.Join(root, "saida")
	var got []string
	if err := filepath.WalkDir(saida, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == saida {
			return err
		}
		rel, _ := filepath.Rel(saida, p)
		got = append(got, rel)
		if d.Type()&fs.ModeSymlink != 0 {
			t.Errorf("symlink created: %s", rel)
		}
		if strings.Contains(rel, ReservedPrefix) {
			t.Errorf("reserved name extracted: %s", rel)
		}
		if _, err := Normalize(rel); err != nil {
			t.Errorf("unaddressable path extracted: %s (%v)", rel, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range got {
		if g == "bom.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legitimate entry missing: %v", got)
	}
}

// O modo declarado no arquivo é ignorado: setuid dentro de um .zip não pode virar setuid no disco.
func TestExtractIgnoresDeclaredMode(t *testing.T) {
	r, root, outside := fixture(t)
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	makeZip(t, root, "modes.zip", []zipEntry{
		{name: "setuid.sh", body: "#!/bin/sh\n", mode: 0o4755},
		{name: "todos.txt", body: "x", mode: 0o777},
		{name: "pasta/", body: "", mode: 0o2777 | fs.ModeDir},
	})
	if _, err := r.ExtractZip(context.Background(), "modes.zip", "saida", bigLimits, nil); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"setuid.sh", "todos.txt"} {
		fi, err := os.Lstat(filepath.Join(root, "saida", n))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o644 || fi.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
			t.Errorf("%s: mode %v", n, fi.Mode())
		}
	}
	fi, err := os.Lstat(filepath.Join(root, "saida", "pasta"))
	if err != nil || fi.Mode().Perm() != 0o755 || fi.Mode()&fs.ModeSetgid != 0 {
		t.Fatalf("pasta: %v %v", fi.Mode(), err)
	}
	checkCanary(t, outside)
}

// O tamanho descomprimido do cabeçalho é escolhido por quem monta o arquivo. Só os bytes que
// realmente passam pelo disco contam, e o teto corta a extração no meio.
func TestExtractBombs(t *testing.T) {
	r, root, outside := fixture(t)
	ctx := context.Background()
	if err := r.Mkdir("s1"); err != nil {
		t.Fatal(err)
	}
	// Cabeçalho mentindo um terabyte com dez bytes de conteúdo. O tamanho declarado não pode
	// virar nem arquivo gigante nem espaço reservado: a entrada é recusada por estar corrompida,
	// e o que fica no disco é nada.
	makeZip(t, root, "mentira.zip", []zipEntry{
		{name: "grande.bin", body: "0123456789", raw: true, method: zip.Store, declare: 1 << 40},
		{name: "normal.txt", body: "conteudo real"},
	})
	res, err := r.ExtractZip(ctx, "mentira.zip", "s1", bigLimits, nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "s1", "grande.bin")); !os.IsNotExist(err) {
		t.Fatalf("entry with a lying header produced a file: %v", err)
	}
	if res.Bytes > 1000 {
		t.Fatalf("declared size trusted: %d bytes counted", res.Bytes)
	}
	// E o arquivo legítimo ao lado sai com o tamanho real, sem blocos reservados a mais.
	fi, err := os.Stat(filepath.Join(root, "s1", "normal.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 13 {
		t.Fatalf("normal.txt size %d", fi.Size())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Blocks > 64 {
		t.Fatalf("preallocated %d blocks for a 13-byte file", st.Blocks)
	}

	// Bomba de verdade: muitos zeros, que comprimem bem. O teto corta.
	if err := r.Mkdir("s2"); err != nil {
		t.Fatal(err)
	}
	makeZip(t, root, "bomba.zip", []zipEntry{{name: "zeros.bin", body: strings.Repeat("\x00", 4<<20)}})
	if _, err := r.ExtractZip(ctx, "bomba.zip", "s2", ExtractLimits{MaxBytes: 1 << 16, MaxEntries: 10}, nil); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("bomb: want ErrArchiveLimit, got %v", err)
	}
	// O arquivo parcial não fica com nome legítimo no disco.
	if _, err := os.Stat(filepath.Join(root, "s2", "zeros.bin")); !os.IsNotExist(err) {
		t.Fatalf("partial file left behind: %v", err)
	}

	// Entradas demais: recusado antes de escrever qualquer coisa.
	if err := r.Mkdir("s3"); err != nil {
		t.Fatal(err)
	}
	many := make([]zipEntry, 20)
	for i := range many {
		many[i] = zipEntry{name: fmt.Sprintf("f%d.txt", i), body: "x"}
	}
	makeZip(t, root, "muitas.zip", many)
	if _, err := r.ExtractZip(ctx, "muitas.zip", "s3", ExtractLimits{MaxBytes: 1 << 20, MaxEntries: 5}, nil); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("entry cap: %v", err)
	}
	if des, _ := os.ReadDir(filepath.Join(root, "s3")); len(des) != 0 {
		t.Fatalf("wrote %d entries despite the cap", len(des))
	}
	checkCanary(t, outside)
}

// Entrada cifrada, método não suportado e duplicada viram aviso — nunca arquivo de lixo no disco.
func TestExtractSkipsUnreadable(t *testing.T) {
	r, root, outside := fixture(t)
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	makeZip(t, root, "ruins.zip", []zipEntry{
		{name: "cifrado.txt", body: "texto cifrado ilegivel", raw: true, method: zip.Store, flags: 0x1, declare: 22},
		{name: "deflate64.bin", body: "xxxx", raw: true, method: 9, declare: 4},
		{name: "dup.txt", body: "primeira"},
		{name: "dup.txt", body: "segunda"},
	})
	res, err := r.ExtractZip(context.Background(), "ruins.zip", "saida", bigLimits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.Skipped != 3 {
		t.Fatalf("result: %+v", res)
	}
	// A entrada cifrada não pode ter virado um arquivo com o texto cifrado dentro.
	if _, err := os.Stat(filepath.Join(root, "saida", "cifrado.txt")); !os.IsNotExist(err) {
		t.Fatal("encrypted entry was written to disk")
	}
	if _, err := os.Stat(filepath.Join(root, "saida", "deflate64.bin")); !os.IsNotExist(err) {
		t.Fatal("unsupported method left a file behind")
	}
	// A duplicada não sobrescreve a primeira.
	if b, _ := os.ReadFile(filepath.Join(root, "saida", "dup.txt")); string(b) != "primeira" {
		t.Fatalf("duplicate overwrote: %q", b)
	}
	checkCanary(t, outside)
}

// Zips do Windows trazem os acentos em CP437. Sem converter, todo arquivo com acento seria
// recusado como UTF-8 inválido — o que, em português, é quase todo arquivo.
func TestExtractDecodesLegacyNames(t *testing.T) {
	r, root, _ := fixture(t)
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	// "Orçamento.pdf" em CP437: ç = 0x87, a = 0x61...
	legacy := "Or\x87amento.txt"
	makeZip(t, root, "windows.zip", []zipEntry{{name: legacy, body: "conteudo"}})
	res, err := r.ExtractZip(context.Background(), "windows.zip", "saida", bigLimits, nil)
	if err != nil || res.Files != 1 {
		t.Fatalf("extract: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(root, "saida", "Orçamento.txt")); err != nil {
		des, _ := os.ReadDir(filepath.Join(root, "saida"))
		names := []string{}
		for _, d := range des {
			names = append(names, d.Name())
		}
		t.Fatalf("CP437 name not decoded, got %v", names)
	}
}

// Cancelar no meio devolve context.Canceled, e quem chamou apaga a pasta.
func TestExtractCancels(t *testing.T) {
	r, root, outside := fixture(t)
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	many := make([]zipEntry, 50)
	for i := range many {
		many[i] = zipEntry{name: fmt.Sprintf("f%d.txt", i), body: strings.Repeat("x", 1000)}
	}
	makeZip(t, root, "muitas.zip", many)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.ExtractZip(ctx, "muitas.zip", "saida", bigLimits, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	checkCanary(t, outside)
}

// Ida e volta com o zip que o próprio produto gera: o endurecimento não pode ter quebrado o caso
// de uso número um, que é baixar um zip do Filezam e extraí-lo de volta.
func TestExtractRoundTrip(t *testing.T) {
	r, root, _ := fixture(t)
	ctx := context.Background()
	var buf bytes.Buffer
	if err := r.WriteZip(ctx, &buf, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "saida.zip"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("saida"); err != nil {
		t.Fatal(err)
	}
	res, err := r.ExtractZip(ctx, "saida.zip", "saida", bigLimits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 {
		t.Fatalf("round trip: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "saida", "a", "file.txt")); string(b) != "hello" {
		t.Fatalf("content: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "saida", "a", "sub", "deep.txt")); string(b) != "deep" {
		t.Fatalf("nested content: %q", b)
	}
}
