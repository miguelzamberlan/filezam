package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		MaxParallel: 4, ShareMaxTTL: 720 * time.Hour, Fsync: false, SecureCookies: config.SecureOff, UploadStaleAge: time.Hour}
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
	bob.expect("POST", "/api/shares", map[string]any{"path": "docs/hi.txt", "expiresIn": 60}, 409)
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
