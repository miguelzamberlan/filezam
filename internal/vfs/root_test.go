package vfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
