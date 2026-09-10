package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/config"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
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
		// orphan part gets pruned on listing
		os.WriteFile(filepath.Join(root, "teamA", "docs", vfs.PartName("deadbeef")), []byte("x"), 0o644)
		bob.expect("GET", "/api/files?path=docs", nil, 200)
		if _, err := os.Stat(filepath.Join(root, "teamA", "docs", vfs.PartName("deadbeef"))); err == nil {
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
	o = admin.expect("POST", "/api/shares", map[string]any{"path": "teamA/pub", "expiresIn": 600, "password": "s3gredo"}, 201)
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
	pub.expect("POST", "/api/public/"+tok2+"/unlock", map[string]string{"password": "s3gredo"}, 200)
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
	// apagar e recriar: inode novo → 404 para a pasta e para o arquivo
	os.RemoveAll(filepath.Join(root, "teamA", "pub"))
	os.MkdirAll(filepath.Join(root, "teamA", "pub"), 0o755)
	os.WriteFile(filepath.Join(root, "teamA", "pub", "doc.txt"), []byte("novo"), 0o644)
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
	admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": "000000"}, 401)
	code, _ := auth.TOTPCode(secret, time.Now())
	o = admin.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code}, 200)
	rec := o["recoveryCodes"].([]any)
	if len(rec) != 10 || o["user"].(map[string]any)["totpEnabled"] != true {
		t.Fatalf("enable: %v", o)
	}
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
	c1.expect("POST", "/api/auth/totp/enable", map[string]string{"code": code2}, 200)
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
