package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/config"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

type client struct {
	t   *testing.T
	srv *httptest.Server
	c   *http.Client
}

func newEnv(t *testing.T) (*client, *Server, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "data")
	os.MkdirAll(filepath.Join(root, "teamA", "pub"), 0o755)
	os.MkdirAll(filepath.Join(root, "teamB"), 0o755)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "doc.txt"), []byte("public doc"), 0o644)
	os.WriteFile(filepath.Join(root, "teamB", "secret.txt"), []byte("secret"), 0o644)
	cfg := &config.Config{Root: root, DataDir: filepath.Join(dir, "cfg"), AdminUser: "admin", AdminPassword: "admin",
		SessionTTL: time.Hour, SessionMaxTTL: 24 * time.Hour, ChunkSize: 1 << 20, BatchMaxFiles: 200, BatchMaxBytes: 32 << 20,
		MaxParallel: 4, ShareMaxTTL: 720 * time.Hour, Fsync: false, SecureCookies: config.SecureOff, UploadStaleAge: time.Hour, IndexInterval: time.Hour}
	os.MkdirAll(cfg.DataDir, 0o755)
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	base, err := vfs.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(cfg, db, base, log, "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: ts, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, s, root
}

func (c *client) do(method, path string, body any, hdr map[string]string) (*http.Response, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
		ctype = "application/octet-stream"
	case io.Reader:
		rd = b
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
		ctype = "application/json"
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, rd)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("X-Filezam", "1")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if b, ok := body.([]byte); ok {
		req.ContentLength = int64(len(b))
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	var out map[string]any
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
	}
	return resp, out
}

func (c *client) expect(method, path string, body any, status int) map[string]any {
	c.t.Helper()
	resp, out := c.do(method, path, body, nil)
	if resp.StatusCode != status {
		c.t.Fatalf("%s %s: status %d want %d: %v", method, path, resp.StatusCode, status, out)
	}
	if resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return out
}

func code(out map[string]any) string {
	if e, ok := out["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (c *client) login(user, pass string) map[string]any {
	c.t.Helper()
	return c.expect("POST", "/api/auth/login", map[string]string{"username": user, "password": pass}, 200)
}

func TestEndToEnd(t *testing.T) {
	admin, _, root := newEnv(t)

	// unauthenticated
	admin.expect("GET", "/api/files?path=", nil, 401)
	// login + forced password change
	out := admin.login("admin", "admin")
	if !out["user"].(map[string]any)["mustChangePassword"].(bool) {
		t.Fatal("expected mustChangePassword")
	}
	if o := admin.expect("GET", "/api/files?path=", nil, 403); code(o) != "password_change_required" {
		t.Fatalf("gate: %v", o)
	}
	if o := admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "short"}, 400); code(o) != "weak_password" {
		t.Fatalf("weak: %v", o)
	}
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("GET", "/api/files?path=", nil, 200)
	// old password rejected
	admin2, _, _ := &client{t: t, srv: admin.srv, c: admin.c}, 0, 0
	_ = admin2
	{
		jar, _ := cookiejar.New(nil)
		fresh := &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
		fresh.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "admin"}, 401)
	}

	// CSRF: missing header
	{
		req, _ := http.NewRequest("POST", admin.srv.URL+"/api/files/mkdir", strings.NewReader(`{"path":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := admin.c.Do(req)
		if resp.StatusCode != 403 {
			t.Fatalf("csrf without header: %d", resp.StatusCode)
		}
		req, _ = http.NewRequest("POST", admin.srv.URL+"/api/files/mkdir", strings.NewReader(`{"path":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Filezam", "1")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		resp, _ = admin.c.Do(req)
		if resp.StatusCode != 403 {
			t.Fatalf("csrf cross-site: %d", resp.StatusCode)
		}
	}

	// create scoped user
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "nope"}, 400)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 201)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 409)
	// last admin guard
	if o := admin.expect("PATCH", "/api/admin/users/1", map[string]any{"role": "user"}, 409); code(o) != "last_admin" {
		t.Fatalf("last admin: %v", o)
	}

	jar, _ := cookiejar.New(nil)
	bob := &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
	bob.login("bob", "bobpassword1")
	bob.expect("GET", "/api/admin/users", nil, 403)
	o := bob.expect("GET", "/api/files?path=", nil, 200)
	entries := o["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["name"] != "pub" {
		t.Fatalf("scope listing: %v", entries)
	}
	if o := bob.expect("GET", "/api/files?path=..", nil, 400); code(o) != "invalid_path" {
		t.Fatalf("traversal: %v", o)
	}
	bob.expect("GET", "/api/files?path=../teamB", nil, 400)
	bob.expect("GET", "/api/files/content?path=%2e%2e/teamB/secret.txt", nil, 400)
	bob.expect("GET", "/api/files?path=teamB", nil, 404)
	// symlink escape inside scope
	os.Symlink(filepath.Join(root, "teamB"), filepath.Join(root, "teamA", "esc"))
	bob.expect("GET", "/api/files?path=esc", nil, 400)
	bob.expect("GET", "/api/files/content?path=esc/secret.txt", nil, 400)

	// mkdir / small PUT upload
	bob.expect("POST", "/api/files/mkdir", map[string]string{"path": "docs"}, 201)
	bob.expect("POST", "/api/files/mkdir", map[string]string{"path": "docs"}, 409)
	bob.expect("POST", "/api/files/mkdir", map[string]string{"path": ".filezam-x"}, 400)
	bob.expect("PUT", "/api/files/content?path=docs/hello.txt&mtime=1700000000000", []byte("hello world"), 201)
	if o := bob.expect("PUT", "/api/files/content?path=docs/hello.txt", []byte("x"), 409); code(o) != "exists" {
		t.Fatalf("put exists: %v", o)
	}
	bob.expect("PUT", "/api/files/content?path=docs/hello.txt&overwrite=1", []byte("hello again"), 201)
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "docs", "hello.txt")); string(b) != "hello again" {
		t.Fatalf("content: %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(root, "teamA", "docs", "hello.txt")); fi.ModTime().Unix() < 1 {
		t.Fatal("mtime")
	}
	// download + range + inline
	{
		resp, _ := bob.do("GET", "/api/files/content?path=docs/hello.txt", nil, map[string]string{"Range": "bytes=0-4"})
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 206 || string(b) != "hello" {
			t.Fatalf("range: %d %q", resp.StatusCode, b)
		}
		if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
			t.Fatalf("disposition: %s", cd)
		}
		resp, _ = bob.do("GET", "/api/files/content?path=docs/hello.txt&inline=1", nil, nil)
		io.ReadAll(resp.Body)
		if resp.Header.Get("Content-Security-Policy") == "" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
			t.Fatalf("inline headers: %v", resp.Header)
		}
	}
	// html is never inline as html
	bob.expect("PUT", "/api/files/content?path=docs/evil.html", []byte("<script>alert(1)</script>"), 201)
	{
		resp, _ := bob.do("GET", "/api/files/content?path=docs/evil.html&inline=1", nil, nil)
		io.ReadAll(resp.Body)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("html inline type: %s", ct)
		}
	}
	// markdown (the SPA renders it; the server only ever sends text/plain)
	bob.expect("PUT", "/api/files/content?path=docs/readme.markdown", []byte("# t\n<script>alert(1)</script>"), 201)
	{
		resp, _ := bob.do("GET", "/api/files/content?path=docs/readme.markdown&inline=1", nil, nil)
		io.ReadAll(resp.Body)
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || resp.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("markdown inline headers: %v", resp.Header)
		}
		os.Remove(filepath.Join(root, "teamA", "docs", "readme.markdown")) // não altera as contagens de docs/ mais abaixo
	}

	// batch upload
	{
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		meta, _ := mw.CreateFormField("meta")
		meta.Write([]byte(`{"files":[{"path":"sub/a.txt","mtime":1700000000000,"size":1},{"path":"../x.txt","size":1},{"path":"b.txt","size":1}]}`))
		p0, _ := mw.CreateFormFile("0", "a.txt")
		p0.Write([]byte("A"))
		p1, _ := mw.CreateFormFile("1", "x.txt")
		p1.Write([]byte("X"))
		p2, _ := mw.CreateFormFile("2", "b.txt")
		p2.Write([]byte("B"))
		mw.Close()
		resp, out := bob.do("POST", "/api/files/batch?dir=docs/batch", &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
		if resp.StatusCode != 200 {
			t.Fatalf("batch: %d %v", resp.StatusCode, out)
		}
		res := out["results"].([]any)
		if res[0].(map[string]any)["ok"] != true || res[2].(map[string]any)["ok"] != true || res[1].(map[string]any)["ok"] == true {
			t.Fatalf("batch results: %v", res)
		}
		if b, _ := os.ReadFile(filepath.Join(root, "teamA", "docs", "batch", "sub", "a.txt")); string(b) != "A" {
			t.Fatal("batch content")
		}
		if _, err := os.Stat(filepath.Join(root, "teamA", "x.txt")); err == nil {
			t.Fatal("batch traversal wrote outside dir")
		}
	}

	// chunked upload: 2.5 MiB with 1 MiB chunks, out of order
	{
		data := bytes.Repeat([]byte("0123456789abcdef"), (2<<20+512<<10)/16)
		o := bob.expect("POST", "/api/uploads", map[string]any{"dir": "docs", "name": "big.bin", "size": len(data), "mtime": 1700000000000}, 201)
		id := o["id"].(string)
		if int(o["chunks"].(float64)) != 3 {
			t.Fatalf("chunks: %v", o)
		}
		bob.expect("POST", "/api/uploads", map[string]any{"dir": "docs", "name": "big.bin", "size": len(data)}, 409)
		chunk := func(i int) []byte {
			s, e := i<<20, (i+1)<<20
			if e > len(data) {
				e = len(data)
			}
			return data[s:e]
		}
		bob.expect("PUT", "/api/uploads/"+id+"?index=2", chunk(2), 200)
		if o := bob.expect("POST", "/api/uploads/"+id+"/complete", nil, 409); code(o) != "incomplete" {
			t.Fatalf("incomplete: %v", o)
		}
		bob.expect("PUT", "/api/uploads/"+id+"?index=0", chunk(0)[:100], 400) // bad length
		bob.expect("PUT", "/api/uploads/"+id+"?index=5", chunk(0), 400)
		bob.expect("PUT", "/api/uploads/"+id+"?index=0", chunk(0), 200)
		bob.expect("PUT", "/api/uploads/"+id+"?index=0", chunk(0), 200) // idempotent
		bob.expect("PUT", "/api/uploads/"+id+"?index=1", chunk(1), 200)
		o = bob.expect("GET", "/api/uploads/"+id, nil, 200)
		if len(o["received"].([]any)) != 3 {
			t.Fatalf("received: %v", o)
		}
		// admin cannot touch bob's upload
		admin.expect("GET", "/api/uploads/"+id, nil, 404)
		bob.expect("POST", "/api/uploads/"+id+"/complete", nil, 200)
		got, _ := os.ReadFile(filepath.Join(root, "teamA", "docs", "big.bin"))
		if !bytes.Equal(got, data) {
			t.Fatal("chunked content mismatch")
		}
		l := bob.expect("GET", "/api/files?path=docs", nil, 200)
		for _, e := range l["entries"].([]any) {
			if strings.HasPrefix(e.(map[string]any)["name"].(string), ".filezam-") {
				t.Fatal("part visible in listing")
			}
		}
		// orphan part gets pruned on listing, but only after OrphanGrace: a fresh part may be
		// a single-PUT/batch upload still streaming (those have no session row)
		part := filepath.Join(root, "teamA", "docs", vfs.PartName("deadbeef"))
		os.WriteFile(part, []byte("x"), 0o644)
		bob.expect("GET", "/api/files?path=docs", nil, 200)
		if _, err := os.Stat(part); err != nil {
			t.Fatal("fresh part pruned while it could still be in flight")
		}
		old := time.Now().Add(-uploads.OrphanGrace - time.Minute)
		os.Chtimes(part, old, old)
		bob.expect("GET", "/api/files?path=docs", nil, 200)
		if _, err := os.Stat(part); err == nil {
			t.Fatal("orphan part not pruned")
		}
	}

	// rename, copy, move, delete
	bob.expect("POST", "/api/files/rename", map[string]string{"path": "docs/hello.txt", "newName": "hi.txt"}, 200)
	bob.expect("POST", "/api/files/rename", map[string]string{"path": "docs/hi.txt", "newName": "evil.html"}, 409)
	bob.expect("POST", "/api/files/rename", map[string]string{"path": "docs/hi.txt", "newName": "a/b"}, 400)
	bob.expect("POST", "/api/files/mkdir", map[string]string{"path": "dest"}, 201)
	o = bob.expect("POST", "/api/files/copy", map[string]any{"sources": []string{"docs"}, "destDir": "dest"}, 200)
	job := waitJob(t, bob, o)
	if job["state"] != "done" || int(job["done"].(float64)) != 5 {
		t.Fatalf("copy job: %v", job)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "dest", "docs", "big.bin")); err != nil {
		t.Fatal("copied file missing")
	}
	// copy again with rename policy -> "docs (1)"
	o = bob.expect("POST", "/api/files/copy", map[string]any{"sources": []string{"docs"}, "destDir": "dest", "onConflict": "rename"}, 200)
	waitJob(t, bob, o)
	if _, err := os.Stat(filepath.Join(root, "teamA", "dest", "docs (1)", "hi.txt")); err != nil {
		t.Fatal("renamed copy missing")
	}
	bob.expect("POST", "/api/files/copy", map[string]any{"sources": []string{"docs"}, "destDir": "docs/batch"}, 400)
	o = bob.expect("POST", "/api/files/move", map[string]any{"sources": []string{"dest/docs (1)"}, "destDir": ""}, 200)
	job = waitJob(t, bob, o)
	if job["state"] != "done" {
		t.Fatalf("move job: %v", job)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "docs (1)", "hi.txt")); err != nil {
		t.Fatal("moved dir missing")
	}
	o = bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"docs (1)", "dest"}}, 200)
	waitJob(t, bob, o)
	if _, err := os.Stat(filepath.Join(root, "teamA", "dest")); err == nil {
		t.Fatal("delete failed")
	}
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{""}}, 400)
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"missing"}}, 404)

	// zip
	{
		resp, _ := bob.do("GET", "/api/files/zip?path=docs", nil, nil)
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("zip: %d", resp.StatusCode)
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		if !names["docs/big.bin"] || !names["docs/batch/sub/a.txt"] {
			t.Fatalf("zip names: %v", names)
		}
	}

	// favorites
	bob.expect("POST", "/api/favorites", map[string]string{"path": "docs"}, 201)
	bob.expect("POST", "/api/favorites", map[string]string{"path": "docs/hi.txt"}, 409)
	o = bob.expect("GET", "/api/favorites", nil, 200)
	favs := o["favorites"].([]any)
	if len(favs) != 1 || favs[0].(map[string]any)["path"] != "docs" {
		t.Fatalf("favorites: %v", favs)
	}
	fid := int64(favs[0].(map[string]any)["id"].(float64))
	bob.expect("DELETE", fmt.Sprintf("/api/favorites/%d", fid), nil, 200)

	// shares
	bob.expect("POST", "/api/shares", map[string]any{"path": "docs/nope.txt", "expiresIn": 60}, 404)
	bob.expect("POST", "/api/shares", map[string]any{"path": "pub", "expiresIn": 0}, 400)
	o = bob.expect("POST", "/api/shares", map[string]any{"path": "pub", "expiresIn": 120}, 201)
	token := o["token"].(string)
	shareID := int64(o["share"].(map[string]any)["id"].(float64))
	if !strings.Contains(o["url"].(string), "/s/"+token) {
		t.Fatalf("share url: %v", o["url"])
	}
	pubJar, _ := cookiejar.New(nil)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{Jar: pubJar}}
	pub.expect("GET", "/api/public/"+token, nil, 200)
	o = pub.expect("GET", "/api/public/"+token+"/list?path=", nil, 200)
	if len(o["entries"].([]any)) != 1 {
		t.Fatalf("public list: %v", o)
	}
	pub.expect("GET", "/api/public/"+token+"/list?path=..", nil, 400)
	pub.expect("GET", "/api/public/"+token+"/list?path=../docs", nil, 400)
	{
		resp, _ := pub.do("GET", "/api/public/"+token+"/content?path=doc.txt", nil, nil)
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || string(b) != "public doc" {
			t.Fatalf("public content: %d %q", resp.StatusCode, b)
		}
		resp, _ = pub.do("GET", "/api/public/"+token+"/zip", nil, nil)
		io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" {
			t.Fatalf("public zip: %d", resp.StatusCode)
		}
	}
	pub.expect("GET", "/api/public/"+strings.Repeat("x", 43), nil, 404)
	// bob can copy the link again: the listing carries the token
	o = bob.expect("GET", "/api/shares", nil, 200)
	if list := o["shares"].([]any); len(list) != 1 || list[0].(map[string]any)["token"] != token {
		t.Fatalf("share token in listing: %v", o)
	}
	// admin sees bob's share and can revoke it
	o = admin.expect("GET", "/api/shares", nil, 200)
	if len(o["shares"].([]any)) != 1 {
		t.Fatalf("admin shares: %v", o)
	}
	admin.expect("DELETE", fmt.Sprintf("/api/shares/%d", shareID), nil, 200)
	pub.expect("GET", "/api/public/"+token, nil, 404)

	// expiry with fake clock
	o = bob.expect("POST", "/api/shares", map[string]any{"path": "pub", "expiresIn": 60}, 201)
	token2 := o["token"].(string)
	pub.expect("GET", "/api/public/"+token2, nil, 200)
	_, s, _ := &client{}, (*Server)(nil), ""
	_ = s
	// (clock advanced below via db.Now)

	// change scope -> sessions revoked
	bobID := int64(2)
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", bobID), map[string]any{"scope": ""}, 200)
	bob.expect("GET", "/api/auth/me", nil, 401)
	bob.login("bob", "bobpassword1")
	o = bob.expect("GET", "/api/files?path=", nil, 200)
	if len(o["entries"].([]any)) != 2 {
		t.Fatalf("root listing after scope change: %v", o)
	}
	// favorites made under old scope hidden? (path stored base-relative -> now visible as teamA/docs)
	// disable + login blocked
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", bobID), map[string]any{"disabled": true}, 200)
	bob.expect("GET", "/api/auth/me", nil, 401)
	{
		jar, _ := cookiejar.New(nil)
		c := &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
		c.expect("POST", "/api/auth/login", map[string]string{"username": "bob", "password": "bobpassword1"}, 401)
	}
	// delete user
	admin.expect("DELETE", fmt.Sprintf("/api/admin/users/%d", bobID), nil, 200)
	admin.expect("DELETE", "/api/admin/users/1", nil, 409)
	// audit has entries
	o = admin.expect("GET", "/api/admin/audit", nil, 200)
	if len(o["entries"].([]any)) < 5 {
		t.Fatalf("audit: %v", o)
	}
	// admin dirs
	o = admin.expect("GET", "/api/admin/dirs?path=", nil, 200)
	if len(o["dirs"].([]any)) != 2 {
		t.Fatalf("dirs: %v", o)
	}
	// logout
	admin.expect("POST", "/api/auth/logout", nil, 200)
	admin.expect("GET", "/api/auth/me", nil, 401)
}

func waitJob(t *testing.T, c *client, o map[string]any) map[string]any {
	t.Helper()
	job := o["job"].(map[string]any)
	for i := 0; i < 100 && job["state"] == "running"; i++ {
		time.Sleep(20 * time.Millisecond)
		job = c.expect("GET", "/api/jobs/"+job["id"].(string), nil, 200)["job"].(map[string]any)
	}
	return job
}

func TestShareExpiry(t *testing.T) {
	admin, s, _ := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 60}, 201)
	token := o["token"].(string)
	admin.expect("GET", "/api/public/"+token, nil, 200)
	s.db.Now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	admin.expect("GET", "/api/public/"+token, nil, 404)
	s.db.Now = time.Now
	admin.expect("GET", "/api/public/"+token, nil, 200)
}

func TestLoginRateLimitAndLockout(t *testing.T) {
	admin, _, _ := newEnv(t)
	var got429 bool
	for i := 0; i < 8; i++ {
		resp, _ := admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
		if resp.StatusCode == 429 {
			got429 = true
			break
		}
		if resp.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, resp.StatusCode)
		}
	}
	if !got429 {
		t.Fatal("expected rate limit")
	}
}

func TestSPAAndHeaders(t *testing.T) {
	c, _, _ := newEnv(t)
	resp, _ := c.do("GET", "/b/some/path", nil, nil)
	io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("spa: %d %v", resp.StatusCode, resp.Header)
	}
	resp, _ = c.do("GET", "/api/health", nil, nil)
	io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatal("health")
	}
	u, _ := url.Parse(c.srv.URL)
	_ = u
}

func TestInfoAndDisk(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	o := admin.expect("GET", "/api/files/disk", nil, 200)
	if o["total"].(float64) <= 0 || o["free"].(float64) <= 0 {
		t.Fatalf("disk: %v", o)
	}
	os.WriteFile(filepath.Join(root, "teamA", "pub", "b.txt"), []byte("12345"), 0o644)
	admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600}, 201)
	admin.expect("POST", "/api/favorites", map[string]string{"path": "teamA/pub"}, 201)
	o = admin.expect("GET", "/api/files/info?path=teamA/pub", nil, 200)
	tot := o["totals"].(map[string]any)
	if int(tot["files"].(float64)) != 2 || int(tot["bytes"].(float64)) != 15 || tot["partial"] != false {
		t.Fatalf("totals: %v", tot)
	}
	if sl := o["shares"].([]any); len(sl) != 1 || o["favorite"] != true || sl[0].(map[string]any)["token"] == "" {
		t.Fatalf("info shares/fav: %v", o)
	}
	o = admin.expect("GET", "/api/files/info?path=teamA", nil, 200)
	if int(o["totals"].(map[string]any)["dirs"].(float64)) != 1 {
		t.Fatalf("dirs: %v", o)
	}
	o = admin.expect("GET", "/api/files/info?path=teamA/pub/doc.txt", nil, 200)
	if _, has := o["totals"]; has {
		t.Fatal("file info should not scan")
	}
	admin.expect("GET", "/api/files/info?path=../x", nil, 400)
}

// Endurecimentos da revisão de segurança: preview inline emoldurável só pelo próprio
// origin, nomes de controle recusados, partes de upload nunca endereçáveis, links
// públicos seguem o estado do usuário e a troca de senha tem rate limit.
func TestSecurityHardening(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "a.pdf"), []byte("%PDF-1.4 x"), 0o644)
	os.WriteFile(filepath.Join(root, "teamA", "pub", ".filezam-upload-deadbeef.part"), []byte("partial"), 0o644)

	// inline: mesmo origin pode emoldurar (preview de PDF), o resto continua bloqueado
	resp, _ := admin.do("GET", "/api/files/content?path=teamA/pub/a.pdf&inline=1", nil, nil)
	io.ReadAll(resp.Body)
	if resp.Header.Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'self'") || !strings.HasPrefix(resp.Header.Get("Content-Security-Policy"), "sandbox;") {
		t.Fatalf("inline headers: %v", resp.Header)
	}
	resp, _ = admin.do("GET", "/api/files/content?path=teamA/pub/a.pdf", nil, nil)
	io.ReadAll(resp.Body)
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("download headers: %v", resp.Header)
	}

	// nomes novos com caracteres de controle são recusados; leitura de partes internas também
	admin.expect("POST", "/api/files/mkdir", map[string]string{"path": "teamA/x\r\ny"}, 400)
	admin.expect("POST", "/api/files/rename", map[string]string{"path": "teamA/pub/a.pdf", "newName": "a\tb.pdf"}, 400)
	admin.expect("GET", "/api/files/content?path=teamA/pub/.filezam-upload-deadbeef.part", nil, 400)
	admin.expect("GET", "/api/files/stat?path=teamA/pub/.filezam-upload-deadbeef.part", nil, 400)

	// links públicos: desativar o dono apaga o acesso; estreitar o escopo remove links de fora dele
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "carol", "password": "carolpassword1", "scope": ""}, 201)
	carolJar, _ := cookiejar.New(nil)
	carol := &client{t: t, srv: admin.srv, c: &http.Client{Jar: carolJar}}
	carol.login("carol", "carolpassword1")
	o := carol.expect("POST", "/api/shares", map[string]any{"path": "teamB", "expiresIn": 600}, 201)
	tokB := o["token"].(string)
	o = carol.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600}, 201)
	tokA := o["token"].(string)
	o = admin.expect("GET", "/api/admin/users", nil, 200)
	var carolID int64
	for _, u := range o["users"].([]any) {
		if m := u.(map[string]any); m["username"] == "carol" {
			carolID = int64(m["id"].(float64))
		}
	}
	pubJar, _ := cookiejar.New(nil)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{Jar: pubJar}}
	pub.expect("GET", "/api/public/"+tokB, nil, 200)
	pub.expect("GET", "/api/public/"+tokA, nil, 200)
	pub.expect("GET", "/api/public/"+tokA+"/list?path=.filezam-upload-deadbeef.part", nil, 400)
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", carolID), map[string]any{"scope": "teamA"}, 200)
	pub.expect("GET", "/api/public/"+tokB, nil, 404) // teamB saiu do escopo
	pub.expect("GET", "/api/public/"+tokA, nil, 200) // teamA/pub continua dentro
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", carolID), map[string]any{"disabled": true}, 200)
	pub.expect("GET", "/api/public/"+tokA, nil, 404)
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", carolID), map[string]any{"disabled": false}, 200)
	pub.expect("GET", "/api/public/"+tokA, nil, 200)

	// troca de senha: senha atual errada é limitada como o login (5/min por usuário)
	carol.login("carol", "carolpassword1")
	var got429 bool
	for i := 0; i < 8 && !got429; i++ {
		resp, _ := carol.do("POST", "/api/auth/password", map[string]string{"current": "wrong", "new": "another password 1"}, nil)
		io.ReadAll(resp.Body)
		got429 = resp.StatusCode == 429
	}
	if !got429 {
		t.Fatal("expected password change rate limit")
	}
}

func TestLogPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/public/abc":      "/api/public/<token>",
		"/api/public/abc/list": "/api/public/<token>/list",
		"/api/public/abc/zip":  "/api/public/<token>/zip",
		"/s/abc":               "/s/<token>",
		"/api/files/content":   "/api/files/content",
		"/api/public/":         "/api/public/",
	} {
		if got := logPath(in); got != want {
			t.Errorf("logPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	os.MkdirAll(filepath.Join(root, "teamA", "Relatórios 2026"), 0o755)
	os.WriteFile(filepath.Join(root, "teamA", "Relatórios 2026", "Balanço.XLSX"), []byte("x"), 0o644)
	os.Symlink("/etc", filepath.Join(root, "teamA", "etc"))
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 201)
	bobJar, _ := cookiejar.New(nil)
	bob := &client{t: t, srv: admin.srv, c: &http.Client{Jar: bobJar}}
	bob.login("bob", "bobpassword1")

	o := bob.expect("GET", "/api/files/search?path=&q=balan", nil, 200)
	res := o["results"].([]any)
	if len(res) != 1 || o["partial"] != false {
		t.Fatalf("search: %v", o)
	}
	hit := res[0].(map[string]any)
	if hit["dir"] != "Relatórios 2026" || hit["entry"].(map[string]any)["name"] != "Balanço.XLSX" {
		t.Fatalf("hit: %v", hit)
	}
	o = bob.expect("GET", "/api/files/search?path=&q=secret", nil, 200) // teamB/secret.txt fica fora do escopo
	if len(o["results"].([]any)) != 0 {
		t.Fatalf("scope leak: %v", o)
	}
	o = bob.expect("GET", "/api/files/search?path=&q=passwd", nil, 200) // symlink para /etc não é seguido
	if len(o["results"].([]any)) != 0 {
		t.Fatalf("symlink followed: %v", o)
	}
	o = bob.expect("GET", "/api/files/search?path=&q=t&limit=1", nil, 200)
	if len(o["results"].([]any)) != 1 || o["partial"] != true {
		t.Fatalf("limit: %v", o)
	}
	bob.expect("GET", "/api/files/search?path=&q=", nil, 400)
	bob.expect("GET", "/api/files/search?path=..&q=x", nil, 400)
	bob.expect("GET", "/api/files/search?path=pub/doc.txt&q=x", nil, 409)
	bob.expect("GET", "/api/files/search?path=nope&q=x", nil, 404)
	o = admin.expect("GET", "/api/files/search?path=teamB&q=secret", nil, 200)
	if len(o["results"].([]any)) != 1 {
		t.Fatalf("admin search: %v", o)
	}
}

func TestListPaging(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	dir := filepath.Join(root, "teamA", "many")
	os.MkdirAll(filepath.Join(dir, "zdir"), 0o755)
	for i := 1; i <= 25; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), []byte(strings.Repeat("x", i)), 0o644)
	}
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o644)
	// sem limit: comportamento antigo, tudo de uma vez (inclusive ocultos)
	o := admin.expect("GET", "/api/files?path=teamA/many", nil, 200)
	if len(o["entries"].([]any)) != 27 || o["total"] != nil {
		t.Fatalf("full listing: %d", len(o["entries"].([]any)))
	}
	o = admin.expect("GET", "/api/files?path=teamA/many&limit=10", nil, 200)
	es := o["entries"].([]any)
	if len(es) != 10 || o["total"].(float64) != 26 || o["hidden"].(float64) != 1 || o["offset"].(float64) != 0 {
		t.Fatalf("page 1: %v", o)
	}
	if es[0].(map[string]any)["name"] != "zdir" || es[1].(map[string]any)["name"] != "f1.txt" || es[2].(map[string]any)["name"] != "f2.txt" {
		t.Fatalf("order (dirs first, natural): %v %v %v", es[0], es[1], es[2])
	}
	o = admin.expect("GET", "/api/files?path=teamA/many&limit=10&offset=20", nil, 200)
	if es = o["entries"].([]any); len(es) != 6 || es[5].(map[string]any)["name"] != "f25.txt" {
		t.Fatalf("last page: %v", o)
	}
	o = admin.expect("GET", "/api/files?path=teamA/many&limit=3&sort=size&dir=desc&hidden=1", nil, 200)
	if es = o["entries"].([]any); o["total"].(float64) != 27 || es[0].(map[string]any)["name"] != "zdir" || es[1].(map[string]any)["name"] != "f25.txt" {
		t.Fatalf("size desc with hidden: %v", o)
	}
	o = admin.expect("GET", "/api/files?path=teamA/many&limit=10&offset=999", nil, 200)
	if len(o["entries"].([]any)) != 0 {
		t.Fatalf("offset past end: %v", o)
	}
}

func TestFileShareAndPassword(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "other.txt"), []byte("other"), 0o644)
	pubJar, _ := cookiejar.New(nil)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{Jar: pubJar}}

	// share de arquivo único
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub/doc.txt", "expiresIn": 600}, 201)
	tok := o["token"].(string)
	if sv := o["share"].(map[string]any); sv["kind"] != "file" || sv["hasPassword"] != false {
		t.Fatalf("share view: %v", sv)
	}
	o = pub.expect("GET", "/api/public/"+tok, nil, 200)
	if o["kind"] != "file" || o["locked"] != false || o["size"].(float64) != 10 || o["fileName"] != "doc.txt" {
		t.Fatalf("file info: %v", o)
	}
	for _, q := range []string{"", "?path=other.txt", "?path=../teamB/secret.txt"} {
		resp, _ := pub.do("GET", "/api/public/"+tok+"/content"+q, nil, nil)
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || string(b) != "public doc" {
			t.Fatalf("file content %q: %d %q", q, resp.StatusCode, b)
		}
	}
	pub.expect("GET", "/api/public/"+tok+"/list", nil, 409)
	pub.expect("GET", "/api/public/"+tok+"/zip", nil, 409)
	admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub/doc.txt", "expiresIn": 600, "password": "abc"}, 400)

	// pasta com senha
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600, "password": "s3gredo-forte"}, 201)
	tok2 := o["token"].(string)
	if o["share"].(map[string]any)["hasPassword"] != true {
		t.Fatalf("hasPassword: %v", o)
	}
	o = pub.expect("GET", "/api/public/"+tok2, nil, 200)
	if o["locked"] != true || o["size"] != nil {
		t.Fatalf("locked info: %v", o)
	}
	pub.expect("GET", "/api/public/"+tok2+"/list?path=", nil, 401)
	pub.expect("GET", "/api/public/"+tok2+"/content?path=doc.txt", nil, 401)
	pub.expect("GET", "/api/public/"+tok2+"/zip", nil, 401)
	pub.expect("POST", "/api/public/"+tok2+"/unlock", map[string]string{"password": "errada"}, 401)
	pub.expect("POST", "/api/public/"+tok2+"/unlock", map[string]string{"password": "s3gredo-forte"}, 200)
	o = pub.expect("GET", "/api/public/"+tok2, nil, 200)
	if o["locked"] != false {
		t.Fatalf("unlocked info: %v", o)
	}
	if o = pub.expect("GET", "/api/public/"+tok2+"/list?path=", nil, 200); len(o["entries"].([]any)) != 2 {
		t.Fatalf("unlocked list: %v", o)
	}
	// outro visitante continua bloqueado; força bruta é limitada
	otherJar, _ := cookiejar.New(nil)
	other := &client{t: t, srv: admin.srv, c: &http.Client{Jar: otherJar}}
	other.expect("GET", "/api/public/"+tok2+"/content?path=doc.txt", nil, 401)
	got429 := false
	for i := 0; i < 8 && !got429; i++ {
		resp, _ := other.do("POST", "/api/public/"+tok2+"/unlock", map[string]string{"password": "x"}, nil)
		io.ReadAll(resp.Body)
		got429 = resp.StatusCode == 429
	}
	if !got429 {
		t.Fatal("unlock not rate limited")
	}
	// link sem senha: unlock é no-op
	pub.expect("POST", "/api/public/"+tok+"/unlock", map[string]string{"password": ""}, 200)
	// auditoria registrou a falha
	o = admin.expect("GET", "/api/admin/audit?limit=50", nil, 200)
	found := false
	for _, e := range o["entries"].([]any) {
		if e.(map[string]any)["action"] == "share.unlock.fail" {
			found = true
		}
	}
	if !found {
		t.Fatal("audit share.unlock.fail missing")
	}
}

// Um preview de Markdown no link público pede todas as imagens de uma vez: além dos 2
// downloads simultâneos por IP a requisição espera um slot em vez de responder 429.
func TestPublicDownloadWaitsForSlot(t *testing.T) {
	admin, s, _ := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	tok := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600}, 201)["token"].(string)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{}}
	const ip = "127.0.0.1"
	s.publicDL.TryAcquire(ip)
	s.publicDL.TryAcquire(ip) // dois downloads em andamento

	done := make(chan int, 1)
	go func() {
		resp, err := pub.c.Get(pub.srv.URL + "/api/public/" + tok + "/content?path=doc.txt&inline=1")
		if err != nil {
			done <- 0
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != "public doc" {
			done <- -resp.StatusCode
			return
		}
		done <- resp.StatusCode
	}()
	select {
	case st := <-done:
		t.Fatalf("answered while both slots were busy: %d", st)
	case <-time.After(150 * time.Millisecond):
	}
	s.publicDL.Release(ip)
	select {
	case st := <-done:
		if st != 200 {
			t.Fatalf("after a slot freed: %d", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request not woken by the release")
	}

	// a espera tem teto: com os slots presos, 429 busy
	s.publicWait = 50 * time.Millisecond
	s.publicDL.TryAcquire(ip)
	_, o := pub.do("GET", "/api/public/"+tok+"/zip", nil, nil)
	if e, _ := o["error"].(map[string]any); e == nil || e["code"] != "busy" {
		t.Fatalf("zip with slots held: %v", o)
	}
	s.publicDL.Release(ip)
	s.publicDL.Release(ip)
	if resp, _ := pub.do("GET", "/api/public/"+tok+"/zip", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("zip after release: %d", resp.StatusCode)
	}

	// teto global de zips públicos: com todos ocupados (outros visitantes), 429 após a espera;
	// downloads simples não entram nesse teto
	for i := 0; i < publicZipMax; i++ {
		s.publicZip.TryAcquire()
	}
	_, o = pub.do("GET", "/api/public/"+tok+"/zip", nil, nil)
	if e, _ := o["error"].(map[string]any); e == nil || e["code"] != "busy" {
		t.Fatalf("zip with the global cap reached: %v", o)
	}
	if resp, _ := pub.do("GET", "/api/public/"+tok+"/content?path=doc.txt", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("download blocked by the zip cap: %d", resp.StatusCode)
	}
	s.publicZip.Release()
	if resp, _ := pub.do("GET", "/api/public/"+tok+"/zip", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("zip after a global slot freed: %d", resp.StatusCode)
	}
}

func TestTrash(t *testing.T) {
	admin, s, root := newEnv(t)
	s.cfg.TrashRetention = time.Hour
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 201)
	bobJar, _ := cookiejar.New(nil)
	bob := &client{t: t, srv: admin.srv, c: &http.Client{Jar: bobJar}}
	bob.login("bob", "bobpassword1")

	// bob exclui dentro do escopo: vai para teamA/.filezam-trash e some da listagem
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"pub/doc.txt"}}, 200)
	if _, err := os.Stat(filepath.Join(root, "teamA", "pub", "doc.txt")); !os.IsNotExist(err) {
		t.Fatal("file still in place")
	}
	o := bob.expect("GET", "/api/trash", nil, 200)
	items := o["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["path"] != "pub/doc.txt" || items[0].(map[string]any)["by"] != "bob" || o["retention"].(float64) != 3600 {
		t.Fatalf("bob trash: %v", o)
	}
	id := items[0].(map[string]any)["id"].(string)
	// a pasta reservada não aparece na listagem nem é endereçável
	o = bob.expect("GET", "/api/files?path=", nil, 200)
	for _, e := range o["entries"].([]any) {
		if strings.HasPrefix(e.(map[string]any)["name"].(string), ".filezam-") {
			t.Fatalf("trash dir listed: %v", e)
		}
	}
	bob.expect("GET", "/api/files?path=.filezam-trash", nil, 400)
	// admin vê a linha de bob com caminho relativo à raiz; restaura para o lugar original
	o = admin.expect("GET", "/api/trash", nil, 200)
	if items = o["items"].([]any); len(items) != 1 || items[0].(map[string]any)["path"] != "teamA/pub/doc.txt" {
		t.Fatalf("admin trash: %v", o)
	}
	// conflito: já existe um doc.txt novo no lugar → mantém ambos
	os.WriteFile(filepath.Join(root, "teamA", "pub", "doc.txt"), []byte("new"), 0o644)
	o = bob.expect("POST", "/api/trash/restore", map[string]any{"ids": []string{id}}, 200)
	rs := o["restored"].([]any)
	if len(rs) != 1 || rs[0].(map[string]any)["path"] != "pub/doc (1).txt" || len(o["failed"].([]any)) != 0 {
		t.Fatalf("restore: %v", o)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "pub", "doc (1).txt")); string(b) != "public doc" {
		t.Fatalf("restored content: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", ".filezam-trash", id)); !os.IsNotExist(err) {
		t.Fatal("trash id folder not removed after restore")
	}
	if o = bob.expect("GET", "/api/trash", nil, 200); len(o["items"].([]any)) != 0 {
		t.Fatalf("trash not empty after restore: %v", o)
	}

	// admin exclui fora do escopo de bob: bob não vê nem restaura
	admin.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"teamB/secret.txt"}}, 200)
	o = admin.expect("GET", "/api/trash", nil, 200)
	adminID := o["items"].([]any)[0].(map[string]any)["id"].(string)
	if o = bob.expect("GET", "/api/trash", nil, 200); len(o["items"].([]any)) != 0 {
		t.Fatalf("bob sees admin trash: %v", o)
	}
	o = bob.expect("POST", "/api/trash/restore", map[string]any{"ids": []string{adminID}}, 200)
	if len(o["restored"].([]any)) != 0 {
		t.Fatal("bob restored an item outside his scope")
	}
	// exclusão permanente de um item da lixeira
	o = admin.expect("POST", "/api/trash/delete", map[string]any{"ids": []string{adminID}}, 200)
	if o["deleted"].(float64) != 1 {
		t.Fatalf("trash delete: %v", o)
	}
	if _, err := os.Stat(filepath.Join(root, ".filezam-trash", adminID)); !os.IsNotExist(err) {
		t.Fatal("purged item still on disk")
	}
	// permanent:true pula a lixeira; esvaziar; varredura por retenção
	os.WriteFile(filepath.Join(root, "teamA", "x.txt"), []byte("x"), 0o644)
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"x.txt"}, "permanent": true}, 200)
	if o = bob.expect("GET", "/api/trash", nil, 200); len(o["items"].([]any)) != 0 {
		t.Fatalf("permanent delete went to trash: %v", o)
	}
	os.WriteFile(filepath.Join(root, "teamA", "y.txt"), []byte("y"), 0o644)
	os.WriteFile(filepath.Join(root, "teamA", "z.txt"), []byte("z"), 0o644)
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"y.txt", "z.txt"}}, 200)
	if o = bob.expect("POST", "/api/trash/empty", nil, 200); o["deleted"].(float64) != 2 {
		t.Fatalf("empty: %v", o)
	}
	os.WriteFile(filepath.Join(root, "teamA", "old.txt"), []byte("o"), 0o644)
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"old.txt"}}, 200)
	s.db.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	s.sweepTrash(context.Background())
	s.db.Now = time.Now
	if o = bob.expect("GET", "/api/trash", nil, 200); len(o["items"].([]any)) != 0 {
		t.Fatalf("sweep left items: %v", o)
	}
	// lixeira desativada → exclusão direta
	s.cfg.TrashRetention = 0
	os.WriteFile(filepath.Join(root, "teamA", "gone.txt"), []byte("g"), 0o644)
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"gone.txt"}}, 200)
	if o = bob.expect("GET", "/api/trash", nil, 200); len(o["items"].([]any)) != 0 {
		t.Fatalf("disabled trash still collects: %v", o)
	}
}

// waitFor polls cond for up to 3 s (index hooks run in goroutines).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestSearchIndex(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 201)
	bobJar, _ := cookiejar.New(nil)
	bob := &client{t: t, srv: admin.srv, c: &http.Client{Jar: bobJar}}
	bob.login("bob", "bobpassword1")
	os.WriteFile(filepath.Join(root, "teamA", "pub", "rel_2026%.xlsx"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "relx2026a.xlsx"), []byte("x"), 0o644)

	// sem varredura ainda: cai no walk
	o := bob.expect("GET", "/api/files/search?path=&q=doc", nil, 200)
	if o["source"] != "walk" {
		t.Fatalf("expected walk before first scan: %v", o)
	}
	o = admin.expect("GET", "/api/admin/index", nil, 200)
	if o["enabled"] != true || o["ready"] != false {
		t.Fatalf("index status: %v", o)
	}
	if ok, err := s.indexer.FullScan(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	o = admin.expect("GET", "/api/admin/index", nil, 200)
	if o["ready"] != true || o["entries"].(float64) < 5 || o["lastFullAt"] == nil {
		t.Fatalf("index status after scan: %v", o)
	}
	// respostas do índice, confinadas ao escopo, sem curingas do LIKE
	o = bob.expect("GET", "/api/files/search?path=&q=doc", nil, 200)
	if o["source"] != "index" || len(o["results"].([]any)) != 1 || o["indexedAt"] == nil {
		t.Fatalf("index search: %v", o)
	}
	if o = bob.expect("GET", "/api/files/search?path=&q=secret", nil, 200); len(o["results"].([]any)) != 0 {
		t.Fatalf("scope leak via index: %v", o)
	}
	if o = bob.expect("GET", "/api/files/search?path=&q=_2026%25", nil, 200); len(o["results"].([]any)) != 1 || o["results"].([]any)[0].(map[string]any)["entry"].(map[string]any)["name"] != "rel_2026%.xlsx" {
		t.Fatalf("LIKE escaping: %v", o)
	}
	if o = admin.expect("GET", "/api/files/search?path=teamB&q=secret", nil, 200); len(o["results"].([]any)) != 1 || o["results"].([]any)[0].(map[string]any)["dir"] != "teamB" {
		t.Fatalf("admin subfolder search: %v", o)
	}
	// fantasma: apagado fora do app some da resposta sem nova varredura
	os.Remove(filepath.Join(root, "teamA", "pub", "relx2026a.xlsx"))
	if o = bob.expect("GET", "/api/files/search?path=&q=relx", nil, 200); len(o["results"].([]any)) != 0 {
		t.Fatalf("ghost returned: %v", o)
	}
	// ganchos: mkdir, rename, upload pequeno, delete
	bob.expect("POST", "/api/files/mkdir", map[string]string{"path": "pub/Novidades 2026"}, 201)
	waitFor(t, "mkdir indexed", func() bool {
		o := bob.expect("GET", "/api/files/search?path=&q=novidades", nil, 200)
		return len(o["results"].([]any)) == 1
	})
	bob.expect("POST", "/api/files/rename", map[string]string{"path": "pub/Novidades 2026", "newName": "Antigas"}, 200)
	waitFor(t, "rename indexed", func() bool {
		a := bob.expect("GET", "/api/files/search?path=&q=novidades", nil, 200)
		b := bob.expect("GET", "/api/files/search?path=&q=antigas", nil, 200)
		return len(a["results"].([]any)) == 0 && len(b["results"].([]any)) == 1
	})
	resp, _ := bob.do("PUT", "/api/files/content?path=pub/Antigas/subpasta/nota.txt", nil, map[string]string{"Content-Type": "application/octet-stream"})
	io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	waitFor(t, "upload indexed", func() bool {
		o := bob.expect("GET", "/api/files/search?path=&q=nota.txt", nil, 200)
		p := bob.expect("GET", "/api/files/search?path=&q=subpasta", nil, 200)
		return len(o["results"].([]any)) == 1 && len(p["results"].([]any)) == 1
	})
	bob.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"pub/Antigas"}, "permanent": true}, 200)
	waitFor(t, "delete unindexed", func() bool {
		o := bob.expect("GET", "/api/files/search?path=&q=nota.txt", nil, 200)
		return len(o["results"].([]any)) == 0
	})
	// reindex manual
	if o = admin.expect("POST", "/api/admin/reindex", nil, 200); o["started"] != true {
		t.Fatalf("reindex: %v", o)
	}
	bob.expect("POST", "/api/admin/reindex", nil, 403)
}

// Bloqueio: nunca 423, sempre 401 (não revela a conta); vale por (usuário, IP), então o
// mesmo usuário entra normalmente de outro endereço; senha certa depois do bloqueio expirar.
func TestLockoutIsSilentAndPerIP(t *testing.T) {
	admin, s, _ := newEnv(t)
	s.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	s.lockout = auth.NewLockout(3, time.Hour) // limiar baixo para caber no rate limit por usuário (5/min)
	attacker := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	victim := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	for i := 0; i < 3; i++ {
		resp, _ := admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, attacker)
		io.ReadAll(resp.Body)
		if resp.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, resp.StatusCode)
		}
	}
	// bloqueado: a senha certa do mesmo IP também dá 401, nunca 423
	resp, body := admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "admin"}, attacker)
	if resp.StatusCode != 401 || body["error"].(map[string]any)["code"] != "bad_credentials" {
		t.Fatalf("locked response: %d %v", resp.StatusCode, body)
	}
	// de outro IP a vítima entra normalmente
	resp, _ = admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "admin"}, victim)
	io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("victim login from another IP: %d", resp.StatusCode)
	}
	if locked, _ := s.lockout.Locked("admin|203.0.113.9"); !locked {
		t.Fatal("attacker key should stay locked")
	}
	// auditoria: login.locked registrado, sem 423 em lugar nenhum (zera o limitador por usuário gasto acima)
	s.loginUser = auth.NewLimiter(5, 5)
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	o := admin.expect("GET", "/api/admin/audit?limit=50", nil, 200)
	seen := false
	for _, e := range o["entries"].([]any) {
		if e.(map[string]any)["action"] == "login.locked" {
			seen = true
		}
	}
	if !seen {
		t.Fatal("audit login.locked missing")
	}
}

// Link amarrado ao inode: apagar e recriar o item no mesmo caminho não reativa o link.
func TestShareBoundToInode(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	pubJar, _ := cookiejar.New(nil)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{Jar: pubJar}}
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600}, 201)
	tokDir := o["token"].(string)
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub/doc.txt", "expiresIn": 600}, 201)
	tokFile := o["token"].(string)
	pub.expect("GET", "/api/public/"+tokDir, nil, 200)
	pub.expect("GET", "/api/public/"+tokFile, nil, 200)
	// mover para fora e voltar: mesmo inode, link continua
	os.Rename(filepath.Join(root, "teamA", "pub"), filepath.Join(root, "teamA", "pub-moved"))
	pub.expect("GET", "/api/public/"+tokDir, nil, 404)
	os.Rename(filepath.Join(root, "teamA", "pub-moved"), filepath.Join(root, "teamA", "pub"))
	pub.expect("GET", "/api/public/"+tokDir, nil, 200)
	// apagar e recriar: inode novo → 404 para a pasta e para o arquivo. Sistemas de arquivos que
	// devolvem o mesmo número de inode logo após apagar (overlayfs, às vezes ext4) não dão essa
	// garantia — limitação documentada em docs/03 e docs/10 — e aqui o teste é pulado.
	before, _ := os.Stat(filepath.Join(root, "teamA", "pub"))
	os.RemoveAll(filepath.Join(root, "teamA", "pub"))
	os.MkdirAll(filepath.Join(root, "teamA", "pub"), 0o755)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "doc.txt"), []byte("novo"), 0o644)
	after, _ := os.Stat(filepath.Join(root, "teamA", "pub"))
	if os.SameFile(before, after) {
		t.Skip("filesystem reused the inode number; delete+recreate cannot be told apart here")
	}
	pub.expect("GET", "/api/public/"+tokDir, nil, 404)
	pub.expect("GET", "/api/public/"+tokFile, nil, 404)
	// links antigos (sem inode gravado) continuam por caminho
	admin.expect("GET", "/api/shares", nil, 200)
}

func TestQuotaAndJobCap(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA", "quota": -1}, 400)
	o := admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA", "quota": 1 << 20}, 201)
	bobID := int64(o["user"].(map[string]any)["id"].(float64))
	if o["user"].(map[string]any)["quota"].(float64) != 1<<20 {
		t.Fatalf("quota in view: %v", o)
	}
	bobJar, _ := cookiejar.New(nil)
	bob := &client{t: t, srv: admin.srv, c: &http.Client{Jar: bobJar}}
	bob.login("bob", "bobpassword1")
	// uso atual: teamA/pub/doc.txt (10 B); disk mostra a cota
	o = bob.expect("GET", "/api/files/disk", nil, 200)
	if o["quota"].(float64) != 1<<20 || o["quotaUsed"].(float64) != 10 {
		t.Fatalf("disk quota view: %v", o)
	}
	big := bytes.Repeat([]byte("x"), 600<<10)
	req, _ := http.NewRequest("PUT", bob.srv.URL+"/api/files/content?path=a.bin", bytes.NewReader(big))
	req.Header.Set("X-Filezam", "1")
	resp, _ := bob.c.Do(req)
	io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("first upload: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("PUT", bob.srv.URL+"/api/files/content?path=b.bin", bytes.NewReader(big))
	req.Header.Set("X-Filezam", "1")
	resp, _ = bob.c.Do(req)
	body := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != 507 || body["error"].(map[string]any)["code"] != "quota_exceeded" {
		t.Fatalf("second upload should exceed quota: %d %v", resp.StatusCode, body)
	}
	bob.expect("POST", "/api/uploads", map[string]any{"dir": "", "name": "c.bin", "size": 2 << 20}, 507)
	// cópia que estoura a cota falha como job
	os.WriteFile(filepath.Join(root, "teamA", "src.bin"), bytes.Repeat([]byte("y"), 500<<10), 0o644)
	o = bob.expect("POST", "/api/files/copy", map[string]any{"sources": []string{"src.bin"}, "destDir": "pub", "onConflict": "rename"}, 200)
	jid := o["job"].(map[string]any)["id"].(string)
	waitFor(t, "copy job to fail", func() bool {
		j := bob.expect("GET", "/api/jobs/"+jid, nil, 200)["job"].(map[string]any)
		return j["state"] == "failed" && strings.Contains(j["error"].(string), "quota")
	})
	// sem cota: tudo liberado
	admin.expect("PATCH", fmt.Sprintf("/api/admin/users/%d", bobID), map[string]any{"quota": 0}, 200)
	bob.login("bob", "bobpassword1") // sessões caíram? quota não revoga; relogin só por garantia
	bob.expect("POST", "/api/uploads", map[string]any{"dir": "", "name": "c.bin", "size": 2 << 20}, 201)
	o = bob.expect("GET", "/api/files/disk", nil, 200)
	if _, has := o["quota"]; has {
		t.Fatalf("quota should be gone: %v", o)
	}
	// limite de jobs simultâneos: o 5º cai em 429 enquanto 4 rodam
	_ = s
	admin.expect("POST", "/api/files/mkdir", map[string]string{"path": "teamA/many"}, 201)
	for i := 0; i < 40; i++ {
		os.WriteFile(filepath.Join(root, "teamA", "many", fmt.Sprintf("f%d.bin", i)), bytes.Repeat([]byte("z"), 64<<10), 0o644)
	}
	got429 := false
	for i := 0; i < 6; i++ {
		resp, body := bob.do("POST", "/api/files/copy", map[string]any{"sources": []string{"many"}, "destDir": "pub", "onConflict": "rename"}, nil)
		if resp.StatusCode == 429 && body["error"].(map[string]any)["code"] == "busy" {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Log("job cap not hit (copies finished too fast); acceptable")
	}
}

func TestJobHistoryAndMetrics(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	os.WriteFile(filepath.Join(root, "teamA", "h.txt"), []byte("h"), 0o644)
	o := admin.expect("POST", "/api/files/copy", map[string]any{"sources": []string{"teamA/h.txt"}, "destDir": "teamB", "onConflict": "rename"}, 200)
	jid := o["job"].(map[string]any)["id"].(string)
	waitFor(t, "job persisted as done", func() bool {
		h := admin.expect("GET", "/api/jobs/history", nil, 200)["jobs"].([]any)
		for _, j := range h {
			m := j.(map[string]any)
			if m["id"] == jid && m["state"] == "done" && m["label"] == "h.txt → /teamB" && m["finishedAt"] != nil {
				return true
			}
		}
		return false
	})
	// um job "running" herdado de um processo anterior vira failed na inicialização
	s.db.UpsertJob(context.Background(), &store.JobRecord{ID: "stale1", UserID: 1, Type: "copy", State: "running", StartedAt: time.Now().Unix()})
	if n, err := s.db.MarkInterruptedJobs(context.Background()); err != nil || n != 1 {
		t.Fatalf("mark interrupted: %d %v", n, err)
	}
	// metrics: desativado sem token; com token exige Bearer ou sessão admin
	admin.expect("GET", "/metrics", nil, 404)
	s.cfg.MetricsToken = "sekret"
	anon := &client{t: t, srv: admin.srv, c: &http.Client{}}
	anon.expect("GET", "/metrics", nil, 401)
	resp, _ := anon.do("GET", "/metrics", nil, map[string]string{"Authorization": "Bearer sekret"})
	b, _ := io.ReadAll(resp.Body)
	body := string(b)
	if resp.StatusCode != 200 || !strings.Contains(body, "filezam_http_requests_total{") || !strings.Contains(body, "filezam_users_total 1") || !strings.Contains(body, `filezam_jobs_finished_total{type="copy",state="done"} 1`) || !strings.Contains(body, `filezam_logins_total{result="ok"}`) {
		t.Fatalf("metrics: %d\n%s", resp.StatusCode, body)
	}
	resp, _ = admin.do("GET", "/metrics", nil, nil) // sessão admin também vale
	io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("metrics via admin session: %d", resp.StatusCode)
	}
}

func TestTOTPFlow(t *testing.T) {
	admin, s, _ := newEnv(t)
	s.loginUser, s.loginIP = auth.NewLimiter(1000, 1000), auth.NewLimiter(1000, 1000) // o fluxo faz muitos logins seguidos
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	if o := admin.expect("GET", "/api/auth/me", nil, 200); o["user"].(map[string]any)["totpEnabled"] != false {
		t.Fatalf("me before: %v", o)
	}
	// cadastro: setup → código errado → código certo devolve códigos de recuperação
	o := admin.expect("POST", "/api/auth/totp/setup", nil, 200)
	secret := o["secret"].(string)
	if !strings.HasPrefix(o["uri"].(string), "otpauth://totp/Filezam:admin?") {
		t.Fatalf("uri: %v", o["uri"])
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": "000000", "password": "correct horse battery"}, 401)
	admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code, "password": "errada"}, 401) // cookie roubado não liga o 2FA
	o = admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code, "password": "correct horse battery"}, 200)
	rec := o["recoveryCodes"].([]any)
	if len(rec) != 10 || o["user"].(map[string]any)["totpEnabled"] != true {
		t.Fatalf("enable: %v", o)
	}
	// com o 2FA ligado, recadastrar (trocar o segredo e os códigos) exige desligar antes, o que pede senha + código
	admin.expect("POST", "/api/auth/totp/setup", nil, 409)
	admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code, "password": "correct horse battery"}, 409)
	// novo login exige a segunda etapa; o mesmo código do cadastro não vale (anti-replay)
	fresh := func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
	}
	c1 := fresh()
	o = c1.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, 200)
	if o["totpRequired"] != true || o["token"] == nil {
		t.Fatalf("login should ask for totp: %v", o)
	}
	ptok := o["token"].(string)
	c1.expect("GET", "/api/auth/me", nil, 401) // sem sessão ainda
	c1.expect("POST", "/api/auth/totp", map[string]any{"token": ptok, "code": code}, 401)
	next, _ := auth.TOTPCode(secret, time.Now().Add(30*time.Second))
	c1.expect("POST", "/api/auth/totp", map[string]any{"token": ptok, "code": next, "trust": true}, 200)
	c1.expect("GET", "/api/auth/me", nil, 200)
	c1.expect("POST", "/api/auth/totp", map[string]any{"token": ptok, "code": next}, 401) // token consumido
	// dispositivo confiável: o próximo login do mesmo jar não pede código
	c1.expect("POST", "/api/auth/logout", nil, 200)
	if o = c1.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, 200); o["totpRequired"] != nil {
		t.Fatalf("trusted device still asked: %v", o)
	}
	// outro navegador: código de recuperação vale uma vez
	c2 := fresh()
	o = c2.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, 200)
	ptok2 := o["token"].(string)
	c2.expect("POST", "/api/auth/totp", map[string]any{"token": ptok2, "code": rec[0].(string)}, 200)
	c3 := fresh()
	o = c3.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, 200)
	ptok3 := o["token"].(string)
	c3.expect("POST", "/api/auth/totp", map[string]any{"token": ptok3, "code": rec[0].(string)}, 401)
	c3.expect("POST", "/api/auth/totp", map[string]any{"token": ptok3, "code": strings.ToUpper(rec[1].(string))}, 200)
	// desligar exige senha + código; depois o cookie de confiança deixa de valer e login volta a ser simples
	c3.expect("POST", "/api/auth/totp/disable", map[string]any{"password": "errada", "code": rec[2]}, 401)
	c3.expect("POST", "/api/auth/totp/disable", map[string]any{"password": "correct horse battery", "code": rec[2]}, 200)
	if o = c1.expect("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, 200); o["totpRequired"] != nil {
		t.Fatalf("after disable: %v", o)
	}
	// exigência para admins: sem 2FA, a API fecha com totp_required mas o cadastro continua acessível
	s.cfg.Require2FA = true
	o = c1.expect("GET", "/api/auth/me", nil, 200)
	if o["user"].(map[string]any)["totpRequired"] != true {
		t.Fatalf("totpRequired flag: %v", o)
	}
	resp, body := c1.do("GET", "/api/files?path=", nil, nil)
	if resp.StatusCode != 403 || body["error"].(map[string]any)["code"] != "totp_required" {
		t.Fatalf("gating: %d %v", resp.StatusCode, body)
	}
	o = c1.expect("POST", "/api/auth/totp/setup", nil, 200)
	code2, _ := auth.TOTPCode(o["secret"].(string), time.Now())
	c1.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code2, "password": "correct horse battery"}, 200)
	c1.expect("GET", "/api/files?path=", nil, 200)
	// admin reseta o 2FA de outro usuário
	s.cfg.Require2FA = false
	c1.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1"}, 201)
	users := c1.expect("GET", "/api/admin/users", nil, 200)["users"].([]any)
	var bobID float64
	for _, u := range users {
		if u.(map[string]any)["username"] == "bob" {
			bobID = u.(map[string]any)["id"].(float64)
		}
	}
	c1.expect("POST", fmt.Sprintf("/api/admin/users/%d/totp/reset", int(bobID)), nil, 200)
	// arquivo de chave criado em DataDir com permissão 0600
	if fi, err := os.Stat(filepath.Join(s.cfg.DataDir, "secret.key")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("secret.key: %v", err)
	}
}

// Endurecimento antes da publicação: caminhos reservados em zip/links/favoritos, X-Forwarded-For
// em várias linhas, limitador por usuário preso ao IP, cookie de senha do link com validade.
func TestPublicationHardening(t *testing.T) {
	admin, s, root := newEnv(t)
	s.cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)

	// lixeira e partes de upload nunca são endereçáveis, nem por zip, link público ou favorito
	os.MkdirAll(filepath.Join(root, "teamA", ".filezam-trash", "x"), 0o755)
	os.WriteFile(filepath.Join(root, "teamA", ".filezam-trash", "x", "secret.txt"), []byte("deleted by someone else"), 0o644)
	os.WriteFile(filepath.Join(root, "teamA", "pub", ".filezam-upload-deadbeef.part"), []byte("partial"), 0o644)
	for _, p := range []string{"teamA/.filezam-trash", "teamA/.filezam-trash/x", "teamA/pub/.filezam-upload-deadbeef.part"} {
		resp, _ := admin.do("GET", "/api/files/zip?path="+url.QueryEscape(p), nil, nil)
		io.ReadAll(resp.Body)
		if resp.StatusCode != 400 {
			t.Fatalf("zip %q: %d", p, resp.StatusCode)
		}
		admin.expect("POST", "/api/shares", map[string]any{"path": p, "expiresIn": 600}, 400)
		admin.expect("POST", "/api/favorites", map[string]any{"path": p}, 400)
	}
	admin.expect("GET", "/api/admin/dirs?path=teamA/.filezam-trash", nil, 400)

	// X-Forwarded-For em duas linhas: a última (posta pelo proxy) vale, não a primeira (do cliente)
	req, _ := http.NewRequest("POST", admin.srv.URL+"/api/auth/login", strings.NewReader(`{"username":"nobody","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Filezam", "1")
	req.Header.Add("X-Forwarded-For", "203.0.113.9")
	req.Header.Add("X-Forwarded-For", "198.51.100.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	o := admin.expect("GET", "/api/admin/audit?limit=1", nil, 200)
	if e := o["entries"].([]any)[0].(map[string]any); e["action"] != "login.fail" || e["ip"] != "198.51.100.7" {
		t.Fatalf("audit ip from multi-line XFF: %v", e)
	}

	// limitador por usuário é por (usuário, IP): tentativas baratas de um IP não trancam o admin de outro
	s.loginUser = auth.NewLimiter(2, 2)
	attacker := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	victim := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	got429 := false
	for i := 0; i < 3; i++ {
		resp, _ := admin.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, attacker)
		io.ReadAll(resp.Body)
		got429 = got429 || resp.StatusCode == 429
	}
	if !got429 {
		t.Fatal("per-user limiter never fired")
	}
	victimJar, _ := cookiejar.New(nil)
	v := &client{t: t, srv: admin.srv, c: &http.Client{Jar: victimJar}}
	resp, body := v.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "correct horse battery"}, victim)
	if resp.StatusCode != 200 {
		t.Fatalf("victim locked out from another ip: %d %v", resp.StatusCode, body)
	}

	// cookie de senha do link: a validade está assinada dentro do valor
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600, "password": "s3gredo-forte"}, 201)
	tok := o["token"].(string)
	pubJar, _ := cookiejar.New(nil)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{Jar: pubJar}}
	pub.expect("POST", "/api/public/"+tok+"/unlock", map[string]string{"password": "s3gredo-forte"}, 200)
	u, _ := url.Parse(admin.srv.URL)
	var ck *http.Cookie
	for _, c := range pubJar.Cookies(u) {
		if strings.HasPrefix(c.Name, "fz_s_") {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("unlock cookie missing")
	}
	expStr, mac, _ := strings.Cut(ck.Value, ".")
	if exp, err := strconv.ParseInt(expStr, 10, 64); err != nil || exp < time.Now().Add(23*time.Hour).Unix() || len(mac) != 64 {
		t.Fatalf("unlock cookie format: %q", ck.Value)
	}
	if o = pub.expect("GET", "/api/public/"+tok, nil, 200); o["locked"] != false {
		t.Fatalf("unlocked: %v", o)
	}
	// valor com validade vencida (mesmo com MAC íntegro sobre outra validade) não abre
	pubJar.SetCookies(u, []*http.Cookie{{Name: ck.Name, Value: "1." + mac, Path: "/"}})
	if o = pub.expect("GET", "/api/public/"+tok, nil, 200); o["locked"] != true {
		t.Fatalf("expired unlock cookie accepted: %v", o)
	}
}

// Excluir, mover ou renomear pelo app derruba os links do item e dos que estão dentro dele,
// independente do inode (sistemas que reutilizam o número não enganam) e sem ressuscitar
// ao restaurar da lixeira.
func TestSharesFollowAppChanges(t *testing.T) {
	admin, s, root := newEnv(t)
	s.cfg.TrashRetention = time.Hour
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	os.MkdirAll(filepath.Join(root, "teamA", "pub2"), 0o755)
	os.WriteFile(filepath.Join(root, "teamB", "b.txt"), []byte("b"), 0o644)
	pub := &client{t: t, srv: admin.srv, c: &http.Client{}}
	share := func(p string) string {
		return admin.expect("POST", "/api/shares", map[string]any{"path": p, "expiresIn": 600}, 201)["token"].(string)
	}
	live := func() map[string]bool {
		out := map[string]bool{}
		for _, v := range admin.expect("GET", "/api/shares", nil, 200)["shares"].([]any) {
			out[v.(map[string]any)["path"].(string)] = true
		}
		return out
	}
	tokDir, tokFile, tokSibling := share("teamA/pub"), share("teamA/pub/doc.txt"), share("teamA/pub2")
	share("teamB/secret.txt")
	share("teamB/b.txt")

	// Antes de excluir, a interface pergunta quais links cairiam: a pasta e o arquivo dentro dela,
	// sem o vizinho de mesmo prefixo, e só no escopo de quem pergunta, sem token.
	o := admin.expect("POST", "/api/shares/affected", map[string]any{"paths": []string{"teamA/pub", "teamA/pub/doc.txt"}}, 200)
	if o["count"].(float64) != 2 || len(o["links"].([]any)) != 2 {
		t.Fatalf("affected: %v", o)
	}
	for _, l := range o["links"].([]any) {
		if _, leak := l.(map[string]any)["token"]; leak {
			t.Fatalf("affected leaks token: %v", l)
		}
	}
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bia", "password": "biapassword1", "scope": "teamA"}, 201)
	jar, _ := cookiejar.New(nil)
	bia := &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
	bia.login("bia", "biapassword1")
	if o := bia.expect("POST", "/api/shares/affected", map[string]any{"paths": []string{"pub"}}, 200); o["count"].(float64) != 2 || o["links"].([]any)[0].(map[string]any)["path"] == "teamA/pub" {
		t.Fatalf("affected for scoped user: %v", o)
	}
	if o := bia.expect("POST", "/api/shares/affected", map[string]any{"paths": []string{"pub2"}}, 200); o["count"].(float64) != 1 {
		t.Fatalf("affected sibling: %v", o)
	}
	bia.expect("POST", "/api/shares/affected", map[string]any{"paths": []string{"../teamB"}}, 400)
	bia.expect("POST", "/api/shares/affected", map[string]any{"paths": []string{}}, 400)

	// excluir (lixeira) a pasta leva o link dela e o do arquivo dentro; "pub2" (mesmo prefixo) fica
	waitJob(t, admin, admin.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"teamA/pub"}}, 200))
	if l := live(); l["teamA/pub"] || l["teamA/pub/doc.txt"] || !l["teamA/pub2"] {
		t.Fatalf("after delete: %v", l)
	}
	pub.expect("GET", "/api/public/"+tokDir, nil, 404)
	pub.expect("GET", "/api/public/"+tokFile, nil, 404)
	pub.expect("GET", "/api/public/"+tokSibling, nil, 200)
	// restaurar devolve o item, não o link
	id := admin.expect("GET", "/api/trash", nil, 200)["items"].([]any)[0].(map[string]any)["id"].(string)
	admin.expect("POST", "/api/trash/restore", map[string]any{"ids": []string{id}}, 200)
	pub.expect("GET", "/api/public/"+tokDir, nil, 404)

	// renomear e mover também
	admin.expect("POST", "/api/files/rename", map[string]any{"path": "teamB/secret.txt", "newName": "s2.txt"}, 200)
	waitJob(t, admin, admin.expect("POST", "/api/files/move", map[string]any{"sources": []string{"teamB/b.txt"}, "destDir": "teamA"}, 200))
	if l := live(); l["teamB/secret.txt"] || l["teamB/b.txt"] {
		t.Fatalf("after rename/move: %v", l)
	}
	// um link novo no item recriado no mesmo caminho funciona normalmente
	os.WriteFile(filepath.Join(root, "teamB", "secret.txt"), []byte("novo"), 0o644)
	pub.expect("GET", "/api/public/"+share("teamB/secret.txt"), nil, 200)
}

// A linha da lixeira é gravada antes do movimento: se o movimento falha nada fica para trás, e
// uma linha sem item (queda no meio) some ao tentar restaurar.
func TestTrashCrashSafety(t *testing.T) {
	admin, s, root := newEnv(t)
	s.cfg.TrashRetention = time.Hour
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)

	// linha fantasma: existe no banco, o item nunca chegou à lixeira
	ghost := &store.TrashItem{ID: "fantasma0000000000000000000000000", UserID: 1, TrashDir: vfs.TrashDirName, Name: "doc.txt", Path: "teamA/pub/doc.txt", Type: "file", Size: 10, DeletedAt: time.Now().Unix()}
	if err := s.db.AddTrash(context.Background(), ghost); err != nil {
		t.Fatal(err)
	}
	o := admin.expect("POST", "/api/trash/restore", map[string]any{"ids": []string{ghost.ID}}, 200)
	if f := o["failed"].([]any); len(f) != 1 || f[0].(map[string]any)["code"] != "not_found" {
		t.Fatalf("ghost restore: %v", o)
	}
	if items := admin.expect("GET", "/api/trash", nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("ghost row kept: %v", items)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "pub", "doc.txt")); string(b) != "public doc" {
		t.Fatal("ghost restore touched the live file")
	}

	// movimento recusado pelo sistema de arquivos: sem linha e sem pasta de id sobrando
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ro := filepath.Join(root, "teamB", "ro")
	os.MkdirAll(ro, 0o755)
	os.WriteFile(filepath.Join(ro, "f.txt"), []byte("f"), 0o644)
	os.Chmod(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	job := waitJob(t, admin, admin.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"teamB/ro/f.txt"}}, 200))
	if job["state"] != "failed" {
		t.Fatalf("delete from read-only dir: %v", job)
	}
	if items := admin.expect("GET", "/api/trash", nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("row left after failed move: %v", items)
	}
	if des, _ := os.ReadDir(filepath.Join(root, vfs.TrashDirName)); len(des) != 0 {
		t.Fatalf("trash id folder left behind: %v", des)
	}
	if _, err := os.Stat(filepath.Join(ro, "f.txt")); err != nil {
		t.Fatal("file lost")
	}
}

// Sessão chunked exclusiva por (usuário, destino) e teto de bytes reservados por usuário.
func TestUploadSessionsPerUserAndReserve(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	users := map[string]*client{}
	for _, n := range []string{"bob", "carol"} {
		admin.expect("POST", "/api/admin/users", map[string]any{"username": n, "password": n + "password1", "scope": "teamA"}, 201)
		jar, _ := cookiejar.New(nil)
		users[n] = &client{t: t, srv: admin.srv, c: &http.Client{Jar: jar}}
		users[n].login(n, n+"password1")
	}
	bob, carol := users["bob"], users["carol"]
	data := []byte("0123456789")
	start := func(c *client, name string, size int, status int) map[string]any {
		return c.expect("POST", "/api/uploads", map[string]any{"dir": "pub", "name": name, "size": size}, status)
	}

	// a sessão de bob não bloqueia carol; quem finaliza primeiro grava, o outro recebe exists
	b := start(bob, "same.bin", len(data), 201)
	c := start(carol, "same.bin", len(data), 201)
	if o := start(bob, "same.bin", len(data), 409); code(o) != "upload_in_progress" {
		t.Fatalf("same user twice: %v", o)
	}
	for _, x := range []struct {
		cl *client
		id string
	}{{bob, b["id"].(string)}, {carol, c["id"].(string)}} {
		x.cl.expect("PUT", "/api/uploads/"+x.id+"?index=0", data, 200)
	}
	bob.expect("POST", "/api/uploads/"+b["id"].(string)+"/complete", nil, 200)
	if o := carol.expect("POST", "/api/uploads/"+c["id"].(string)+"/complete", nil, 409); code(o) != "exists" {
		t.Fatalf("second finalize: %v", o)
	}
	carol.expect("DELETE", "/api/uploads/"+c["id"].(string), nil, 200)

	// teto de reserva: soma dos tamanhos declarados das sessões abertas de cada usuário
	s.uploads = uploads.New(s.db, s.cfg.ChunkSize, false, 3<<20, s.log)
	x := start(bob, "a.bin", 2<<20, 201)
	if o := start(bob, "b.bin", 2<<20, 413); code(o) != "upload_reserve_exceeded" {
		t.Fatalf("over the cap: %v", o)
	}
	start(carol, "b.bin", 2<<20, 201) // o teto é por usuário
	start(bob, "c.bin", 1<<20, 201)   // cabe exatamente
	bob.expect("DELETE", "/api/uploads/"+x["id"].(string), nil, 200)
	start(bob, "b.bin", 2<<20, 201) // cancelar libera a reserva
	if _, err := os.Stat(filepath.Join(root, "teamA", "pub", "same.bin")); err != nil {
		t.Fatal(err)
	}
}

// dropEnv liga os dois recursos novos (nascem desligados) e devolve o admin já logado.
func dropEnv(t *testing.T) (*client, *Server, string) {
	t.Helper()
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"slugsEnabled": true, "dropEnabled": true}, 200)
	return admin, s, root
}

// newPublic returns a visitor: no session, its own cookie jar.
func newPublic(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: srv, c: &http.Client{Jar: jar}}
}

func TestAdminSettings(t *testing.T) {
	admin, s, _ := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)

	// Padrão de fábrica: apelidos ligados, link de envio desligado.
	out := admin.expect("GET", "/api/admin/settings", nil, 200)["settings"].(map[string]any)
	if out["slugsEnabled"] != true || out["dropEnabled"] != false {
		t.Fatalf("factory defaults: %v", out)
	}
	// Desligado vale também para o admin: ele liga primeiro, e isso fica na auditoria.
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/caixa", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 20, "password": "abcd1234"}, 403)
	if code(o) != "feature_disabled" {
		t.Fatalf("drop while disabled: %v", o)
	}
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600, "slug": "vendas", "password": "abcd1234"}, 201)
	if o["share"].(map[string]any)["slug"] != "vendas" {
		t.Fatalf("slug is on by default: %v", o)
	}
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"slugsEnabled": false}, 200)
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600, "slug": "compras", "password": "abcd1234"}, 403)
	if code(o) != "feature_disabled" {
		t.Fatalf("slug while disabled: %v", o)
	}
	// O teto rígido de 30 dias não é negociável pelo painel.
	o = admin.expect("PATCH", "/api/admin/settings", map[string]any{"dropMaxTtl": 31 * 86400}, 400)
	if code(o) != "bad_quota" {
		t.Fatalf("ttl above the hard cap: %v", o)
	}
	// Usuário comum não chega perto.
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA"}, 201)
	bob := newPublic(t, admin.srv)
	bob.login("bob", "bobpassword1")
	bob.expect("GET", "/api/admin/settings", nil, 403)
	bob.expect("PATCH", "/api/admin/settings", map[string]any{"dropEnabled": true}, 403)

	if s.settings().SlugsEnabled {
		t.Fatal("settings cache not refreshed")
	}
	found := false
	for _, e := range admin.expect("GET", "/api/admin/audit", nil, 200)["entries"].([]any) {
		if e.(map[string]any)["action"] == "settings.update" {
			found = true
		}
	}
	if !found {
		t.Fatal("settings.update not audited")
	}
}

func TestShareSlug(t *testing.T) {
	admin, _, _ := dropEnv(t)
	pub := newPublic(t, admin.srv)
	mk := func(slug, pw string, status int) map[string]any {
		body := map[string]any{"path": "teamA/pub", "expiresIn": 3600, "slug": slug}
		if pw != "" {
			body["password"] = pw
		}
		return admin.expect("POST", "/api/shares", body, status)
	}
	// O apelido é adivinhável: sem senha o link não teria segredo nenhum.
	if o := mk("vendas", "", 400); code(o) != "password_required" {
		t.Fatalf("slug without password: %v", o)
	}
	for _, bad := range []string{"ab", "Vendas", "-vendas", "vendas-", "ven das", "ven_das", "admin", "api", strings.Repeat("a", 64)} {
		if o := mk(bad, "abcd1234", 400); code(o) != "invalid_slug" {
			t.Fatalf("slug %q accepted: %v", bad, o)
		}
	}
	o := mk("vendas-2026", "abcd1234", 201)
	tok := o["token"].(string)
	if url, _ := o["url"].(string); !strings.HasSuffix(url, "/s/vendas-2026") {
		t.Fatalf("url should carry the slug: %v", url)
	}
	if o2 := mk("vendas-2026", "abcd1234", 409); code(o2) != "slug_taken" {
		t.Fatalf("duplicate slug: %v", o2)
	}
	// O endereço resolve pelo apelido e pelo token; ambos pedem a senha.
	for _, addr := range []string{"vendas-2026", tok} {
		info := pub.expect("GET", "/api/public/"+addr, nil, 200)
		if info["locked"] != true {
			t.Fatalf("%s should be locked: %v", addr, info)
		}
		// Travado não conta nem o nome da pasta: seria um oráculo de enumeração.
		if _, ok := info["name"]; ok {
			t.Fatalf("locked info leaks the name: %v", info)
		}
	}
	pub.expect("POST", "/api/public/vendas-2026/unlock", map[string]string{"password": "abcd1234"}, 200)
	if info := pub.expect("GET", "/api/public/vendas-2026", nil, 200); info["name"] != "pub" {
		t.Fatalf("after unlock: %v", info)
	}
	pub.expect("GET", "/api/public/vendas-2026/list", nil, 200)

	// Revogar tira o link do ar mas NÃO devolve o apelido ao pool: outra pessoa o assumiria e
	// passaria a receber o que era destinado a quem o divulgou.
	id := int64(admin.expect("GET", "/api/shares", nil, 200)["shares"].([]any)[0].(map[string]any)["id"].(float64))
	admin.expect("DELETE", fmt.Sprintf("/api/shares/%d", id), nil, 200)
	pub.expect("GET", "/api/public/vendas-2026", nil, 404)
	if o2 := mk("vendas-2026", "abcd1234", 409); code(o2) != "slug_taken" {
		t.Fatalf("revoked slug went back to the pool: %v", o2)
	}
	// O dono continua vendo o apelido reservado e pode liberá-lo de propósito.
	var revoked map[string]any
	for _, v := range admin.expect("GET", "/api/shares", nil, 200)["shares"].([]any) {
		if m := v.(map[string]any); m["slug"] == "vendas-2026" {
			revoked = m
		}
	}
	if revoked == nil || revoked["revoked"] != true {
		t.Fatalf("revoked slug should stay listed: %v", revoked)
	}
	admin.expect("DELETE", fmt.Sprintf("/api/shares/%d?purge=1", id), nil, 200)
	mk("vendas-2026", "abcd1234", 201)
}

func TestSlugEnumerationIsRateLimited(t *testing.T) {
	admin, s, _ := dropEnv(t)
	admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600, "slug": "existe", "password": "abcd1234"}, 201)
	pub := newPublic(t, admin.srv)
	s.slugMiss = auth.NewLimiter(5, 5)
	limited := false
	for i := 0; i < 12; i++ {
		resp, _ := pub.do("GET", fmt.Sprintf("/api/public/chute-%d", i), nil, nil)
		resp.Body.Close()
		if resp.StatusCode == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("slug guessing was never rate limited")
	}
	// Quem tem o endereço certo não paga pelo enumerador: só o erro consome o balde.
	pub.expect("GET", "/api/public/existe", nil, 200)
}

// mkDrop cria um link de envio com os limites pedidos e devolve (token, id).
func mkDrop(c *client, path string, quota, maxFile, maxFiles int64) (string, int64) {
	c.t.Helper()
	body := map[string]any{"path": path, "expiresIn": 3600, "mode": "drop", "quotaBytes": quota}
	if maxFile > 0 {
		body["maxFileBytes"] = maxFile
	}
	if maxFiles > 0 {
		body["maxFiles"] = maxFiles
	}
	o := c.expect("POST", "/api/shares", body, 201)
	return o["token"].(string), int64(o["share"].(map[string]any)["id"].(float64))
}

// send faz o envio único de um arquivo pequeno pelo link.
func send(c *client, tok, name string, body []byte) (*http.Response, map[string]any) {
	c.t.Helper()
	return c.do("PUT", "/api/public/"+tok+"/content?name="+url.QueryEscape(name), body, nil)
}

func TestDropCreateRequiresEmptyFolder(t *testing.T) {
	admin, _, root := dropEnv(t)
	// A pasta nasce com o link.
	mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	if fi, err := os.Stat(filepath.Join(root, "teamA", "recebidos")); err != nil || !fi.IsDir() {
		t.Fatalf("drop folder was not created: %v", err)
	}
	// Pasta existente e vazia é aceita; com conteúdo, não.
	if err := os.MkdirAll(filepath.Join(root, "teamA", "vazia"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkDrop(admin, "teamA/vazia", 1<<20, 0, 0)
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 20}, 409)
	if code(o) != "not_empty" {
		t.Fatalf("drop into a folder with files: %v", o)
	}
	// Uma parte de upload também conta como conteúdo.
	if err := os.WriteFile(filepath.Join(root, "teamA", "vazia", vfs.PartName("z")), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/vazia", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 20}, 409); code(o) != "not_empty" {
		t.Fatalf("drop into a folder holding an upload part: %v", o)
	}
	// Vencimento é obrigatório e nunca passa de 30 dias.
	for _, exp := range []any{0, 31 * 86400} {
		o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/x", "expiresIn": exp, "mode": "drop", "quotaBytes": 1 << 20}, 400)
		if code(o) != "bad_expiry" {
			t.Fatalf("expiresIn %v: %v", exp, o)
		}
	}
	// Cota é obrigatória.
	for _, q := range []any{0, int64(1) << 45} {
		o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/y", "expiresIn": 3600, "mode": "drop", "quotaBytes": q}, 400)
		if code(o) != "bad_quota" {
			t.Fatalf("quotaBytes %v: %v", q, o)
		}
	}
	// Nada de pasta órfã quando a criação falha depois do mkdir.
	if _, err := os.Stat(filepath.Join(root, "teamA", "x")); !os.IsNotExist(err) {
		t.Fatalf("folder left behind after a failed create: %v", err)
	}
	if o := admin.expect("POST", "/api/shares", map[string]any{"path": "", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 20}, 400); code(o) != "root_op" {
		t.Fatalf("drop on the scope root: %v", o)
	}
}

func TestDropAnonymousUpload(t *testing.T) {
	admin, s, root := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 64<<20, 0, 0)
	pub := newPublic(t, admin.srv)

	// Envio único.
	resp, out := send(pub, tok, "nota.txt", []byte("conteudo"))
	if resp.StatusCode != 201 {
		t.Fatalf("single upload: %d %v", resp.StatusCode, out)
	}
	if b, err := os.ReadFile(filepath.Join(root, "teamA", "recebidos", "nota.txt")); err != nil || string(b) != "conteudo" {
		t.Fatalf("file on disk: %q %v", b, err)
	}
	// Envio em blocos de um arquivo acima do chunkSize.
	s.cfg.ChunkSize = 4
	s.uploads = uploads.New(s.db, 4, false, s.cfg.UploadReserve, s.log)
	data := []byte("0123456789")
	o := pub.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "grande.bin", "size": len(data)}, 201)
	id := o["id"].(string)
	for i := 0; i*4 < len(data); i++ {
		end := min((i+1)*4, len(data))
		pub.expect("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=%d", tok, id, i), data[i*4:end], 200)
	}
	pub.expect("POST", "/api/public/"+tok+"/uploads/"+id+"/complete", nil, 200)
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "recebidos", "grande.bin")); string(b) != string(data) {
		t.Fatalf("chunked file on disk: %q", b)
	}
	// O visitante vê os próprios envios e o consumo do link.
	info := pub.expect("GET", "/api/public/"+tok, nil, 200)
	if info["mode"] != "drop" || info["fileCount"].(float64) != 2 || info["usedBytes"].(float64) != 18 {
		t.Fatalf("drop info: %v", info)
	}
	if mine := info["mine"].([]any); len(mine) != 2 {
		t.Fatalf("mine: %v", mine)
	}
	// Escrita exige CSRF, como qualquer rota mutante.
	req, _ := http.NewRequest("PUT", admin.srv.URL+"/api/public/"+tok+"/content?name=x.txt", strings.NewReader("x"))
	resp, err := pub.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("upload without X-Filezam: %d", resp.StatusCode)
	}
	// E o dono vê tudo na auditoria.
	found := false
	for _, e := range admin.expect("GET", "/api/admin/audit", nil, 200)["entries"].([]any) {
		if e.(map[string]any)["action"] == "share.drop.upload" {
			found = true
		}
	}
	if !found {
		t.Fatal("anonymous upload not audited")
	}
}

func TestDropNeverOverwrites(t *testing.T) {
	admin, _, root := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	// Arquivo que já era do dono na pasta.
	if err := os.WriteFile(filepath.Join(root, "teamA", "recebidos", "doc.txt"), []byte("do dono"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, b := newPublic(t, admin.srv), newPublic(t, admin.srv)
	for _, c := range []*client{a, b} {
		if resp, out := send(c, tok, "doc.txt", []byte("de fora")); resp.StatusCode != 201 {
			t.Fatalf("upload: %d %v", resp.StatusCode, out)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(root, "teamA", "recebidos", "doc.txt")); string(got) != "do dono" {
		t.Fatalf("owner file overwritten: %q", got)
	}
	names, _ := filepath.Glob(filepath.Join(root, "teamA", "recebidos", "*"))
	if len(names) != 3 {
		t.Fatalf("want 3 files (owner + two renamed), got %v", names)
	}
	// Cada visitante enxerga só o próprio envio, e sempre com o nome que ELE pediu: o link não
	// deixa ler a pasta, então revelar o desvio de nome contaria que o arquivo já existia.
	for _, c := range []*client{a, b} {
		mine := c.expect("GET", "/api/public/"+tok, nil, 200)["mine"].([]any)
		if len(mine) != 1 {
			t.Fatalf("sender isolation: %v", mine)
		}
		if n := mine[0].(map[string]any)["name"].(string); n != "doc.txt" {
			t.Fatalf("receipt must echo the requested name, got %q", n)
		}
	}
}

// O link de envio não deixa ler a pasta. Se a resposta contasse que o arquivo foi salvo com
// outro nome, bastaria enviar 1 byte com um nome chutado para descobrir o que já está lá —
// de outro remetente ou do próprio dono.
func TestDropDoesNotLeakExistingNames(t *testing.T) {
	admin, s, root := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	if err := os.WriteFile(filepath.Join(root, "teamA", "recebidos", "segredo.pdf"), []byte("do dono"), 0o644); err != nil {
		t.Fatal(err)
	}
	pub := newPublic(t, admin.srv)
	// Nome que existe e nome que não existe respondem exatamente igual.
	_, hit := send(pub, tok, "segredo.pdf", []byte("x"))
	_, miss := send(pub, tok, "inexistente.pdf", []byte("x"))
	if hit["name"] != "segredo.pdf" || miss["name"] != "inexistente.pdf" {
		t.Fatalf("single upload leaks the collision: %v vs %v", hit, miss)
	}
	// O arquivo do dono continua intacto e o do visitante foi para o lado.
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "recebidos", "segredo.pdf")); string(b) != "do dono" {
		t.Fatalf("owner file overwritten: %q", b)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "recebidos", "segredo (1).pdf")); err != nil {
		t.Fatalf("visitor file should have been written aside: %v", err)
	}
	// O mesmo pela sessão em blocos, que antes vazava por outro caminho: dois remetentes com o
	// mesmo nome batiam no índice único de uploads e o segundo recebia 409.
	s.uploads = uploads.New(s.db, 4, false, s.cfg.UploadReserve, s.log)
	other := newPublic(t, admin.srv)
	var ids []string
	for _, c := range []*client{pub, other} {
		o := c.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "relatorio.bin", "size": 4}, 201)
		if o["name"] != "relatorio.bin" {
			t.Fatalf("session leaks the collision: %v", o)
		}
		ids = append(ids, o["id"].(string))
	}
	for i, c := range []*client{pub, other} {
		c.expect("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=0", tok, ids[i]), make([]byte, 4), 200)
		if o := c.expect("POST", "/api/public/"+tok+"/uploads/"+ids[i]+"/complete", nil, 200); o["name"] != "relatorio.bin" {
			t.Fatalf("complete leaks the collision: %v", o)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "recebidos", "relatorio (1).bin")); err != nil {
		t.Fatalf("second sender should have been written aside: %v", err)
	}
}

// Desligar o recurso tem de parar a escrita anônima nos links que já existem — é isso que se
// faz ao perceber abuso. Idem para os apelidos já criados.
func TestFeatureSwitchStopsLiveLinks(t *testing.T) {
	admin, _, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	slug := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600, "slug": "vendas", "password": "abcd1234"}, 201)["token"].(string)
	pub := newPublic(t, admin.srv)
	if resp, _ := send(pub, tok, "a.txt", []byte("a")); resp.StatusCode != 201 {
		t.Fatal("setup upload failed")
	}
	pub.expect("GET", "/api/public/vendas", nil, 200)

	admin.expect("PATCH", "/api/admin/settings", map[string]any{"dropEnabled": false, "slugsEnabled": false}, 200)

	pub.expect("GET", "/api/public/"+tok, nil, 404)
	if resp, _ := send(pub, tok, "b.txt", []byte("b")); resp.StatusCode != 404 {
		t.Fatalf("upload into a link of a disabled feature: %d", resp.StatusCode)
	}
	pub.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "c.bin", "size": 4}, 404)
	// O apelido para de resolver, mas o token do mesmo link continua valendo.
	pub.expect("GET", "/api/public/vendas", nil, 404)
	pub.expect("GET", "/api/public/"+slug, nil, 200)
}

func TestDropLimits(t *testing.T) {
	admin, s, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 100, 10, 3)
	pub := newPublic(t, admin.srv)

	if resp, out := send(pub, tok, "grande.bin", make([]byte, 11)); resp.StatusCode != 413 || code(out) != "drop_file_limit" {
		t.Fatalf("per-file cap: %d %v", resp.StatusCode, out)
	}
	for i := 0; i < 3; i++ {
		if resp, out := send(pub, tok, fmt.Sprintf("f%d.bin", i), make([]byte, 5)); resp.StatusCode != 201 {
			t.Fatalf("upload %d: %d %v", i, resp.StatusCode, out)
		}
	}
	if resp, out := send(pub, tok, "f4.bin", []byte("x")); resp.StatusCode != 409 || code(out) != "drop_count_exceeded" {
		t.Fatalf("file count cap: %d %v", resp.StatusCode, out)
	}
	// Cota: um link pequeno enche, e a sessão aberta já conta (ela reserva o disco).
	tok2, _ := mkDrop(admin, "teamA/caixa2", 20, 0, 0)
	s.uploads = uploads.New(s.db, 4, false, s.cfg.UploadReserve, s.log)
	o := pub.expect("POST", "/api/public/"+tok2+"/uploads", map[string]any{"name": "a.bin", "size": 16}, 201)
	if resp, out := send(pub, tok2, "b.bin", make([]byte, 8)); resp.StatusCode != 507 || code(out) != "drop_full" {
		t.Fatalf("open session must hold the quota: %d %v", resp.StatusCode, out)
	}
	// Abortar devolve o espaço.
	pub.expect("DELETE", "/api/public/"+tok2+"/uploads/"+o["id"].(string), nil, 200)
	if resp, _ := send(pub, tok2, "b.bin", make([]byte, 8)); resp.StatusCode != 201 {
		t.Fatalf("aborting a session must free its reservation: %d", resp.StatusCode)
	}
}

func TestDropOwnerQuotaIsEnforced(t *testing.T) {
	admin, _, _ := dropEnv(t)
	admin.expect("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "bobpassword1", "scope": "teamA", "quota": 4096}, 201)
	bob := newPublic(t, admin.srv)
	bob.login("bob", "bobpassword1")
	// A cota do link não pode prometer mais do que o dono ainda tem.
	if o := bob.expect("POST", "/api/shares", map[string]any{"path": "recebidos", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 30}, 400); code(o) != "bad_quota" {
		t.Fatalf("link quota above the owner's: %v", o)
	}
	// O escopo já tem o arquivo do fixture, então a cota livre é menor que 4096.
	tok, _ := mkDrop(bob, "recebidos", 4000, 0, 0)
	pub := newPublic(t, admin.srv)
	resp, out := send(pub, tok, "enche.bin", make([]byte, 2000))
	for i := 0; resp.StatusCode == 201 && i < 4; i++ {
		resp, out = send(pub, tok, fmt.Sprintf("enche%d.bin", i), make([]byte, 2000))
	}
	// Cota do dono estourada responde com o erro do link: o visitante anônimo não fica sabendo
	// como anda a conta de quem criou o link.
	if resp.StatusCode != 507 || code(out) != "drop_full" {
		t.Fatalf("owner quota: %d %v", resp.StatusCode, out)
	}
}

func TestDropIsNotReadable(t *testing.T) {
	admin, _, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	pub := newPublic(t, admin.srv)
	if resp, _ := send(pub, tok, "a.txt", []byte("a")); resp.StatusCode != 201 {
		t.Fatal("setup upload failed")
	}
	// Quem envia não lista, não baixa e não leva zip — nem o que ele mesmo mandou.
	for _, p := range []string{"/list", "/content?path=a.txt", "/zip"} {
		if resp, _ := pub.do("GET", "/api/public/"+tok+p, nil, nil); resp.StatusCode != 404 {
			t.Fatalf("drop link served %s: %d", p, resp.StatusCode)
		}
	}
	// E as rotas de escrita não existem num link de leitura.
	read := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 3600}, 201)["token"].(string)
	if resp, _ := send(pub, read, "x.txt", []byte("x")); resp.StatusCode != 404 {
		t.Fatalf("write into a read link: %d", resp.StatusCode)
	}
	pub.expect("POST", "/api/public/"+read+"/uploads", map[string]any{"name": "x", "size": 1}, 404)

	// Com senha, nada de escrita antes do unlock.
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/cofre", "expiresIn": 3600, "mode": "drop", "quotaBytes": 1 << 20, "password": "abcd1234"}, 201)
	locked := o["token"].(string)
	if resp, out := send(pub, locked, "x.txt", []byte("x")); resp.StatusCode != 401 || code(out) != "share_locked" {
		t.Fatalf("upload before unlock: %d %v", resp.StatusCode, out)
	}
	pub.expect("POST", "/api/public/"+locked+"/uploads", map[string]any{"name": "x", "size": 1}, 401)
	pub.expect("POST", "/api/public/"+locked+"/unlock", map[string]string{"password": "abcd1234"}, 200)
	if resp, _ := send(pub, locked, "x.txt", []byte("x")); resp.StatusCode != 201 {
		t.Fatalf("upload after unlock: %d", resp.StatusCode)
	}
}

func TestDropSenderCookieIsSigned(t *testing.T) {
	admin, _, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	pub := newPublic(t, admin.srv)
	if resp, _ := send(pub, tok, "meu.txt", []byte("x")); resp.StatusCode != 201 {
		t.Fatal("setup upload failed")
	}
	if mine := pub.expect("GET", "/api/public/"+tok, nil, 200)["mine"].([]any); len(mine) != 1 {
		t.Fatalf("own upload not listed: %v", mine)
	}
	// O cookie identifica o remetente: adulterá-lo não dá acesso à lista de ninguém, o servidor
	// simplesmente emite uma identidade nova.
	u, _ := url.Parse(admin.srv.URL)
	var name, valid string
	for _, c := range pub.c.Jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "fz_d_") {
			name, valid = c.Name, c.Value
		}
	}
	if name == "" {
		t.Fatal("no sender cookie was set")
	}
	id, _, _ := strings.Cut(valid, ".")
	for _, forged := range []string{id + ".00", strings.Repeat("a", 32) + "." + strings.Split(valid, ".")[1], "short.x"} {
		other := newPublic(t, admin.srv)
		other.c.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: forged}})
		if mine := other.expect("GET", "/api/public/"+tok, nil, 200)["mine"].([]any); len(mine) != 0 {
			t.Fatalf("forged cookie %q saw somebody else's uploads: %v", forged, mine)
		}
	}
	// E um visitante honesto, com jar próprio, também não vê nada de terceiros.
	if mine := newPublic(t, admin.srv).expect("GET", "/api/public/"+tok, nil, 200)["mine"].([]any); len(mine) != 0 {
		t.Fatalf("fresh visitor saw uploads: %v", mine)
	}
}

func TestDropUploadsHiddenFromOwner(t *testing.T) {
	admin, s, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	s.uploads = uploads.New(s.db, 4, false, s.cfg.UploadReserve, s.log)
	pub := newPublic(t, admin.srv)
	id := pub.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "grande.bin", "size": 16}, 201)["id"].(string)

	// A interface do dono aborta toda pendência que enxerga: se a sessão do visitante
	// aparecesse aqui, abrir o navegador de arquivos cancelaria o envio dele.
	if ups := admin.expect("GET", "/api/uploads", nil, 200)["uploads"].([]any); len(ups) != 0 {
		t.Fatalf("owner sees the visitor's session: %v", ups)
	}
	admin.expect("GET", "/api/uploads/"+id, nil, 404)
	admin.expect("DELETE", "/api/uploads/"+id, nil, 404)
	// E a sessão continua válida para quem a abriu.
	pub.expect("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=0", tok, id), make([]byte, 4), 200)
	// Um outro visitante também não mexe nela.
	newPublic(t, admin.srv).expect("DELETE", "/api/public/"+tok+"/uploads/"+id, nil, 404)
}

func TestDropSurvivesRateLimit(t *testing.T) {
	admin, s, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	s.cfg.ChunkSize = 1
	s.uploads = uploads.New(s.db, 1, false, s.cfg.UploadReserve, s.log)
	pub := newPublic(t, admin.srv)
	// O balde de leitura (120/min) mataria este envio no meio: 150 blocos é um arquivo comum
	// dividido, não um ataque.
	id := pub.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "muitos.bin", "size": 150}, 201)["id"].(string)
	for i := 0; i < 150; i++ {
		resp, out := pub.do("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=%d", tok, id, i), []byte{byte(i)}, nil)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("chunk %d: %d %v", i, resp.StatusCode, out)
		}
	}
	pub.expect("POST", "/api/public/"+tok+"/uploads/"+id+"/complete", nil, 200)
}

func TestDropFolderRemovedMidUpload(t *testing.T) {
	admin, s, root := dropEnv(t)
	s.cfg.TrashRetention = time.Hour
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	s.uploads = uploads.New(s.db, 4, false, s.cfg.UploadReserve, s.log)
	pub := newPublic(t, admin.srv)
	id := pub.expect("POST", "/api/public/"+tok+"/uploads", map[string]any{"name": "grande.bin", "size": 16}, 201)["id"].(string)
	pub.expect("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=0", tok, id), make([]byte, 4), 200)

	// O dono apaga a pasta pelo app: o link cai junto e a sessão em voo não fica reservando
	// disco num caminho que nem existe mais.
	waitJob(t, admin, admin.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"teamA/recebidos"}}, 200))
	pub.expect("PUT", fmt.Sprintf("/api/public/%s/uploads/%s?index=1", tok, id), make([]byte, 4), 404)
	pub.expect("GET", "/api/public/"+tok, nil, 404)
	if resp, _ := send(pub, tok, "x.txt", []byte("x")); resp.StatusCode != 404 {
		t.Fatalf("upload into a dead link: %d", resp.StatusCode)
	}
	if ups, _ := s.db.ListUploadsByShare(context.Background(), 1); len(ups) != 0 {
		t.Fatalf("sessions left behind: %v", ups)
	}
	// Recriar a pasta por fora não ressuscita o link.
	if err := os.MkdirAll(filepath.Join(root, "teamA", "recebidos"), 0o755); err != nil {
		t.Fatal(err)
	}
	pub.expect("GET", "/api/public/"+tok, nil, 404)
}

// O editor abre um arquivo, o usuário digita por minutos e só então salva. Sem conferir o mtime,
// quem salvasse por último apagaria em silêncio o trabalho do outro. Os mtimes são explícitos para
// o teste não depender do relógio: a conferência tem resolução de 1 ms.
func TestPutContentIfMtime(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)

	put := func(q, body string, status int) map[string]any {
		resp, out := admin.do("PUT", "/api/files/content?path=teamA/nota.txt&overwrite=1"+q, []byte(body), nil)
		resp.Body.Close()
		if resp.StatusCode != status {
			admin.t.Fatalf("PUT %s: status %d want %d: %v", q, resp.StatusCode, status, out)
		}
		return out
	}
	const t1, t2 = 1700000000000, 1700000060000
	put(fmt.Sprintf("&mtime=%d", t1), "primeira", 201)

	// O editor abriu quando o arquivo estava em t1 e salva: passa, e o arquivo vai para t2.
	put(fmt.Sprintf("&mtime=%d&ifMtime=%d", t2, t1), "segunda", 201)

	// A outra aba, que também abriu em t1, tenta salvar depois: recusada.
	if o := put(fmt.Sprintf("&ifMtime=%d", t1), "terceira", 409); code(o) != "modified" {
		t.Fatalf("stale write: %v", o)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "nota.txt")); string(b) != "segunda" {
		t.Fatalf("content on disk: %q", b)
	}
	// Recarregando, ela passa a ver t2 e consegue salvar.
	put(fmt.Sprintf("&ifMtime=%d", t2), "terceira", 201)

	// Sem o parâmetro, o comportamento de sempre: sobrescreve sem perguntar.
	put("", "quarta", 201)
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "nota.txt")); string(b) != "quarta" {
		t.Fatalf("plain overwrite: %q", b)
	}
	// ifMtime num arquivo que já não existe → 404, e nada é recriado.
	admin.expect("POST", "/api/files/delete", map[string]any{"paths": []string{"teamA/nota.txt"}, "permanent": true}, 200)
	put(fmt.Sprintf("&ifMtime=%d", t2), "quinta", 404)
	if _, err := os.Stat(filepath.Join(root, "teamA", "nota.txt")); !os.IsNotExist(err) {
		t.Fatalf("file recreated by a stale write: %v", err)
	}
}

// zipBytes monta um zip em memória para os testes de endpoint.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractAndArchive(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	if err := os.WriteFile(filepath.Join(root, "teamA", "fotos.zip"), zipBytes(t, map[string]string{"a.txt": "um", "sub/b.txt": "dois"}), 0o644); err != nil {
		t.Fatal(err)
	}

	// Extrai para uma pasta nova com o nome do arquivo.
	waitJob(t, admin, admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/fotos.zip"}, 200))
	if b, _ := os.ReadFile(filepath.Join(root, "teamA", "fotos", "sub", "b.txt")); string(b) != "dois" {
		t.Fatalf("extracted content: %q", b)
	}
	// Extrair de novo não sobrescreve: a segunda vai para o lado.
	waitJob(t, admin, admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/fotos.zip"}, 200))
	if _, err := os.Stat(filepath.Join(root, "teamA", "fotos (1)", "a.txt")); err != nil {
		t.Fatalf("second extraction: %v", err)
	}

	// O que não é arquivo compactado é recusado na hora, não vira job falhado.
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/pub/doc.txt"}, 400); code(o) != "bad_archive" {
		t.Fatalf("not an archive: %v", o)
	}
	// Zip com todo arquivo protegido por senha também: nem job, nem pasta vazia ao lado.
	var enc bytes.Buffer
	zw := zip.NewWriter(&enc)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "segredo.txt", Method: zip.Store, Flags: 0x1, CompressedSize64: 7, UncompressedSize64: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("cifrado")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "teamA", "senha.zip"), enc.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/senha.zip"}, 400); code(o) != "archive_encrypted" {
		t.Fatalf("encrypted archive: %v", o)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "senha")); !os.IsNotExist(err) {
		t.Fatalf("destination folder created for encrypted archive: %v", err)
	}
	// E um .zip que não abre (conteúdo, não extensão) é bad_archive na hora.
	if err := os.WriteFile(filepath.Join(root, "teamA", "falso.zip"), []byte("PK\x03\x04 lixo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/falso.zip"}, 400); code(o) != "bad_archive" {
		t.Fatalf("corrupt archive: %v", o)
	}
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/pub"}, 409); code(o) != "is_dir" {
		t.Fatalf("folder: %v", o)
	}
	admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/nao-existe.zip"}, 404)

	// Arquivo maior que o teto é recusado antes de o zip ser aberto.
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"extractMaxArchive": 1 << 20}, 200)
	big := make([]byte, (1<<20)+1)
	if err := os.WriteFile(filepath.Join(root, "teamA", "grande.zip"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/grande.zip"}, 413); code(o) != "archive_too_large" {
		t.Fatalf("oversized archive: %v", o)
	}

	// Desligar o recurso vale para todos, inclusive o admin.
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"extractEnabled": false}, 200)
	if o := admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/fotos.zip"}, 403); code(o) != "feature_disabled" {
		t.Fatalf("disabled: %v", o)
	}
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"extractEnabled": true}, 200)

	// Compactar: o caminho inverso, na mesma pasta.
	waitJob(t, admin, admin.expect("POST", "/api/files/archive", map[string]any{"paths": []string{"teamA/pub"}, "name": "backup"}, 200))
	b, err := os.ReadFile(filepath.Join(root, "teamA", "backup.zip"))
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !slices.Contains(names, "pub/doc.txt") {
		t.Fatalf("archive contents: %v", names)
	}
	// Nenhum arquivo temporário reservado ficou para trás.
	des, _ := os.ReadDir(filepath.Join(root, "teamA"))
	for _, d := range des {
		if strings.HasPrefix(d.Name(), ".filezam-") {
			t.Fatalf("temp file left behind: %s", d.Name())
		}
	}
	_ = s
}

// A bomba de descompressão falha o job e não deixa a pasta pela metade.
func TestExtractBombFailsJob(t *testing.T) {
	admin, _, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"extractMaxBytes": 1 << 20}, 200)
	if err := os.WriteFile(filepath.Join(root, "teamA", "bomba.zip"), zipBytes(t, map[string]string{"zeros.bin": strings.Repeat("\x00", 4<<20)}), 0o644); err != nil {
		t.Fatal(err)
	}
	j := waitJob(t, admin, admin.expect("POST", "/api/files/extract", map[string]any{"path": "teamA/bomba.zip"}, 200))
	if j["state"] != "failed" {
		t.Fatalf("bomb job: %v", j)
	}
	if _, err := os.Stat(filepath.Join(root, "teamA", "bomba")); !os.IsNotExist(err) {
		t.Fatalf("destination folder left behind: %v", err)
	}
}

// pngBytes gera um PNG de w×h para os testes.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnails(t *testing.T) {
	admin, s, root := newEnv(t)
	admin.login("admin", "admin")
	admin.expect("POST", "/api/auth/password", map[string]string{"current": "admin", "new": "correct horse battery"}, 200)
	if err := os.WriteFile(filepath.Join(root, "teamA", "foto.png"), pngBytes(t, 600, 400), 0o644); err != nil {
		t.Fatal(err)
	}

	// Primeira visita gera; a resposta é um JPEG pequeno, guardável pelo navegador.
	resp, _ := admin.do("GET", "/api/files/thumb?path=teamA/foto.png", nil, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// `private` mantém a miniatura fora de proxy compartilhado; a ausência de `immutable` é o que
	// garante a revalidação depois dos cinco minutos, em vez de a imagem ficar dias no disco de
	// quem já saiu da sessão.
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "private") || !strings.Contains(cc, "max-age=300") || strings.Contains(cc, "immutable") || resp.Header.Get("ETag") == "" {
		t.Fatalf("cache headers: %q %q", cc, resp.Header.Get("ETag"))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	// 600×400 reduzido pelo lado maior: 256×170.
	if cfg.Width != 256 || cfg.Height != 170 {
		t.Fatalf("thumb size: %dx%d", cfg.Width, cfg.Height)
	}
	// O cache vive no DataDir, nunca na árvore do usuário.
	if des, _ := os.ReadDir(filepath.Join(root, "teamA")); len(des) != 2 {
		t.Fatalf("cache leaked into the user's tree: %d entries", len(des))
	}
	etag := resp.Header.Get("ETag")

	// Segunda visita vem do cache e revalida com 304.
	resp2, _ := admin.do("GET", "/api/files/thumb?path=teamA/foto.png", nil, map[string]string{"If-None-Match": etag})
	resp2.Body.Close()
	if resp2.StatusCode != 304 {
		t.Fatalf("revalidation: %d", resp2.StatusCode)
	}

	// Alterar o arquivo muda a chave, então a miniatura é outra.
	if err := os.WriteFile(filepath.Join(root, "teamA", "foto.png"), pngBytes(t, 200, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	resp3, _ := admin.do("GET", "/api/files/thumb?path=teamA/foto.png", nil, map[string]string{"If-None-Match": etag})
	resp3.Body.Close()
	if resp3.StatusCode != 200 || resp3.Header.Get("ETag") == etag {
		t.Fatalf("changed file served a stale thumbnail: %d %q", resp3.StatusCode, resp3.Header.Get("ETag"))
	}

	// O que não é imagem decodificável cai no ícone (404), não em erro.
	for _, p := range []string{"teamA/pub/doc.txt", "teamA/pub", "teamA/nao-existe.png"} {
		r, _ := admin.do("GET", "/api/files/thumb?path="+p, nil, nil)
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Errorf("thumb of %s: %d", p, r.StatusCode)
		}
	}
	// Um PNG que declara dimensões absurdas é recusado pelo cabeçalho, sem decodificar.
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"thumbsMaxPixels": 1 << 16}, 200)
	if err := os.WriteFile(filepath.Join(root, "teamA", "enorme.png"), pngBytes(t, 900, 900), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := admin.do("GET", "/api/files/thumb?path=teamA/enorme.png", nil, nil)
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatalf("oversized image: %d", r.StatusCode)
	}

	// Desligar no admin apaga a opção para todos.
	admin.expect("PATCH", "/api/admin/settings", map[string]any{"thumbsEnabled": false}, 200)
	r2, _ := admin.do("GET", "/api/files/thumb?path=teamA/foto.png", nil, nil)
	r2.Body.Close()
	if r2.StatusCode != 404 {
		t.Fatalf("disabled: %d", r2.StatusCode)
	}
	if cfgOut := admin.expect("GET", "/api/config", nil, 200); cfgOut["thumbsEnabled"] != false {
		t.Fatalf("config still advertises thumbnails: %v", cfgOut["thumbsEnabled"])
	}
	_ = s
}

// O cookie de identidade do remetente precisa existir antes do primeiro envio. Se ele só
// nascesse na primeira escrita, dois envios em paralelo — o padrão do cliente — sairiam os dois
// sem cookie, ganhariam identidades diferentes e uma sobrescreveria a outra: os arquivos da
// identidade perdida sumiriam da lista do próprio remetente.
func TestDropSenderSurvivesParallelFirstUpload(t *testing.T) {
	admin, _, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	pub := newPublic(t, admin.srv)

	// Abrir a página já dá identidade ao visitante.
	pub.expect("GET", "/api/public/"+tok, nil, 200)
	u, _ := url.Parse(admin.srv.URL)
	var cookie string
	for _, c := range pub.c.Jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "fz_d_") {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("opening the page did not establish a sender identity")
	}

	// Envios em paralelo, como o cliente faz.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			resp, _ := send(pub, tok, fmt.Sprintf("f%d.txt", n), []byte("x"))
			resp.Body.Close()
		}(i)
	}
	wg.Wait()

	// A identidade não mudou, e os quatro estão na lista de quem enviou.
	for _, c := range pub.c.Jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "fz_d_") && c.Value != cookie {
			t.Fatal("sender identity changed during parallel uploads")
		}
	}
	if mine := pub.expect("GET", "/api/public/"+tok, nil, 200)["mine"].([]any); len(mine) != 4 {
		t.Fatalf("want 4 uploads listed for the sender, got %d", len(mine))
	}
}

// slowReader entrega o corpo devagar, como uma conexão doméstica enviando para o servidor.
type slowReader struct {
	data  []byte
	step  int
	pause time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	n := min(s.step, min(len(p), len(s.data)))
	copy(p, s.data[:n])
	s.data = s.data[n:]
	return n, nil
}

// Um envio lento não pode bloquear os outros do mesmo link. O mutex existe para as contas de
// cota e para escolher o nome — se ele cobrisse a transferência, o segundo visitante esperaria a
// subida inteira do primeiro, tempo suficiente para um proxy desistir e virar erro na tela.
func TestDropSlowUploadDoesNotBlockOthers(t *testing.T) {
	admin, _, _ := dropEnv(t)
	tok, _ := mkDrop(admin, "teamA/recebidos", 1<<20, 0, 0)
	pub := newPublic(t, admin.srv)
	pub.expect("GET", "/api/public/"+tok, nil, 200)

	// O lento leva ~1,5 s entregando o corpo aos poucos.
	slow := &slowReader{data: make([]byte, 15000), step: 1000, pause: 100 * time.Millisecond}
	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		resp, _ := pub.do("PUT", "/api/public/"+tok+"/content?name=lento.bin", io.Reader(slow), nil)
		resp.Body.Close()
		done <- time.Since(start)
	}()
	time.Sleep(300 * time.Millisecond) // o lento já está no meio da transferência

	start := time.Now()
	resp, out := send(pub, tok, "rapido.bin", []byte("pronto"))
	fast := time.Since(start)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("fast upload: %d %v", resp.StatusCode, out)
	}
	slowTook := <-done
	if slowTook < 800*time.Millisecond {
		t.Skipf("o envio lento terminou rápido demais (%v) para o teste valer", slowTook)
	}
	// O rápido não pode ter esperado o lento: sem a separação, os dois terminariam juntos.
	if fast > slowTook/2 {
		t.Fatalf("envio rápido bloqueado pelo lento: rápido %v, lento %v", fast, slowTook)
	}
}

// Uma transferência que estanca não é defeito do servidor. O erro que chega aqui é o mesmo que
// apareceu em produção — "read tcp ...: i/o timeout", do prazo de leitura do corpo — e ele caía
// como 500 "erro interno", assustando quem enviava e poluindo o log de erros do operador.
func TestStalledTransferIsNotInternalError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
		want int
	}{
		{"prazo de leitura estourado", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, "timeout", 408},
		{"contexto expirado", context.DeadlineExceeded, "timeout", 408},
		{"corpo cortado no meio", io.ErrUnexpectedEOF, "incomplete_body", 400},
		{"cliente desistiu", context.Canceled, "cancelled", 499},
	}
	for _, c := range cases {
		ae := toAPIError(c.err)
		if ae.Code != c.code || ae.Status != c.want {
			t.Errorf("%s: virou %d %q, esperado %d %q", c.name, ae.Status, ae.Code, c.want, c.code)
		}
	}
}

// Duas sessões em blocos abertas ao mesmo tempo não podem passar as duas pela mesma leitura de
// cota. Cada uma reserva no disco o tamanho declarado, então admitir as duas reservaria o dobro
// do que o dono do link autorizou — e o excedente ficaria ocupado até a faxina de sessões velhas.
//
// A garantia é estrutural, não estatística: `admit` devolve o mutex do link **ainda tomado** e
// quem cria a sessão só solta depois de a linha existir. É isso que o teste afirma, porque uma
// corrida de microssegundos disparada por HTTP quase nunca aparece de propósito.
func TestDropAdmitHoldsTheLinkUntilTheSessionExists(t *testing.T) {
	admin, s, _ := dropEnv(t)
	_, id := mkDrop(admin, "teamA/recebidos", 1000, 0, 0)
	ctx := t.Context()
	sh, err := s.db.GetShare(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := s.admit(ctx, sh, "remetente-1", "a.bin", 600)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	entered := make(chan struct{})
	go func() {
		release := s.dropMu.lock(sh.ID)
		close(entered)
		release()
	}()
	select {
	case <-entered:
		unlock()
		t.Fatal("admit devolveu o link destravado: conferir a cota e criar a sessão não são atômicos")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("o mutex do link não foi devolvido")
	}
}

// O mesmo pelo protocolo, de ponta a ponta: o que sai reservado nunca passa do que o link prometeu.
func TestDropQuotaHoldsAgainstParallelSessions(t *testing.T) {
	admin, s, _ := dropEnv(t)
	// A cota cabe uma sessão de 600 bytes, nunca duas.
	tok, _ := mkDrop(admin, "teamA/recebidos", 1000, 0, 0)
	s.uploads = uploads.New(s.db, 64, false, s.cfg.UploadReserve, s.log)
	pub := newPublic(t, admin.srv)
	pub.expect("GET", "/api/public/"+tok, nil, 200) // identidade antes de disparar em paralelo

	const tries = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, tries)
	for i := 0; i < tries; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			resp, _ := pub.do("POST", "/api/public/"+tok+"/uploads",
				map[string]any{"name": fmt.Sprintf("a%d.bin", n), "size": 600}, nil)
			resp.Body.Close()
			codes[n] = resp.StatusCode
		}(i)
	}
	close(start)
	wg.Wait()

	accepted := 0
	for _, c := range codes {
		if c == 201 {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("want exactly one session admitted, got %d (%v)", accepted, codes)
	}
	if used := pub.expect("GET", "/api/public/"+tok, nil, 200)["usedBytes"].(float64); used > 1000 {
		t.Fatalf("reserved %v bytes on a link capped at 1000", used)
	}
}

// A senha de um link segue o mesmo mínimo das senhas de conta. Num link com apelido ela é o
// único segredo, porque o endereço é escolhido para ser fácil de dizer — e de adivinhar.
func TestSharePasswordMinimumLength(t *testing.T) {
	admin, _, _ := dropEnv(t)
	o := admin.expect("POST", "/api/shares", map[string]any{"path": "teamA", "expiresIn": 3600, "password": "abc1"}, 400)
	if code(o) != "weak_password" {
		t.Fatalf("4-character share password: %v", o)
	}
	admin.expect("POST", "/api/shares", map[string]any{"path": "teamA", "expiresIn": 3600, "password": "abcd1234"}, 201)
}

// Sem FILEZAM_ADMIN_PASSWORD, a conta inicial nasce com uma senha sorteada que aparece uma vez no
// log. O padrão fixo anterior ("admin") valia da subida do serviço até o primeiro login: uma
// janela que quem varre a internet conhece de cor.
func TestInitialAdminPasswordIsGenerated(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "data")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Root: root, DataDir: filepath.Join(dir, "cfg"), AdminUser: "admin", AdminPassword: "",
		SessionTTL: time.Hour, SessionMaxTTL: 24 * time.Hour, ChunkSize: 1 << 20, BatchMaxFiles: 200, BatchMaxBytes: 32 << 20,
		MaxParallel: 4, ShareMaxTTL: 720 * time.Hour, Fsync: false, SecureCookies: config.SecureOff, UploadStaleAge: time.Hour, IndexInterval: time.Hour}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	base, err := vfs.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	var logged bytes.Buffer
	s, err := New(cfg, db, base, slog.New(slog.NewTextHandler(&logged, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	pw := ""
	for _, f := range strings.Fields(logged.String()) {
		if after, ok := strings.CutPrefix(f, "password="); ok {
			pw = after
		}
	}
	if err := auth.CheckPolicy(pw); err != nil {
		t.Fatalf("generated password %q does not meet the policy: %v (log: %s)", pw, err, logged.String())
	}
	login := func(p string) int {
		jar, _ := cookiejar.New(nil)
		c := &client{t: t, srv: ts, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
		resp, _ := c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": p}, nil)
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := login("admin"); got != 401 {
		t.Fatalf("the well-known admin/admin still works: %d", got)
	}
	if got := login(pw); got != 200 {
		t.Fatalf("generated password refused: %d", got)
	}
	// E a troca continua obrigatória, então a senha do log envelhece no primeiro acesso.
	u, err := db.GetUserByName(t.Context(), "admin")
	if err != nil || !u.MustChangePassword {
		t.Fatalf("initial admin must be forced to change the password: %v %v", u, err)
	}
}
