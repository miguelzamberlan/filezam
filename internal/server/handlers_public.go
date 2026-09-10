package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

var errShareNotFound = errorf(http.StatusNotFound, "not_found", "link not found or expired")

func (s *Server) isTrustedRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && s.trusted(addr)
}

// publicShare resolves the token to a live share (rate-limited per IP; same 404 for every failure).
func (s *Server) publicShare(r *http.Request) (*store.Share, error) {
	if !s.publicIP.Allow(ipFrom(r)) {
		return nil, errorf(http.StatusTooManyRequests, "rate_limited", "too many requests")
	}
	tok := r.PathValue("token")
	if len(tok) < 16 || len(tok) > 128 {
		return nil, errShareNotFound
	}
	sh, err := s.db.GetActiveShareByToken(r.Context(), auth.HashToken(tok))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errShareNotFound
		}
		return nil, err
	}
	return sh, nil
}

// shareFile is the name of the shared file inside its root (kind "file"), "" for folder shares.
func shareFile(sh *store.Share) string {
	if sh.Kind == "file" {
		return vfs.Base(sh.Path)
	}
	return ""
}

// shareRoot opens the share's root: the folder itself, or, for a file share, its parent
// folder (the file name comes from shareFile; ?path= is never honoured for file shares).
func (s *Server) shareRoot(r *http.Request) (*vfs.Root, *store.Share, error) {
	sh, err := s.publicShare(r)
	if err != nil {
		return nil, nil, err
	}
	dir := sh.Path
	if sh.Kind == "file" {
		dir = vfs.Dir(sh.Path)
	}
	root, err := s.base.Sub(dir)
	if err != nil {
		s.log.Warn("share folder unavailable", "id", sh.ID, "path", sh.Path, "err", err)
		return nil, nil, errShareNotFound
	}
	e, err := root.Stat(shareFile(sh))
	if err != nil || (sh.Kind == "file" && e.Type != "file") || (sh.Kind != "file" && e.Type != "dir") {
		root.Close()
		return nil, nil, errShareNotFound
	}
	// Item substituído (apagado e recriado com o mesmo nome) não herda o link.
	if sh.Ino != 0 {
		if dev, ino, err := root.Identity(shareFile(sh)); err != nil || dev != sh.Dev || ino != sh.Ino {
			root.Close()
			s.log.Info("share target replaced", "id", sh.ID, "path", sh.Path)
			return nil, nil, errShareNotFound
		}
	}
	return root, sh, nil
}

// --- senha do link ---
// O cookie por link guarda HMAC-SHA256(chave = hash Argon2 da senha, mensagem = hash do token):
// só quem passou pelo unlock (e conhece o token) o obtém, e trocar a senha invalida todos.

func shareCookieName(sh *store.Share) string {
	return "fz_s_" + sh.TokenHash[:16]
}

func shareCookieValue(sh *store.Share) string {
	m := hmac.New(sha256.New, []byte(sh.PasswordHash))
	m.Write([]byte(sh.TokenHash))
	return hex.EncodeToString(m.Sum(nil))
}

// shareUnlocked reports whether the request may read a password-protected share.
func shareUnlocked(r *http.Request, sh *store.Share) bool {
	if sh.PasswordHash == "" {
		return true
	}
	c, err := r.Cookie(shareCookieName(sh))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(shareCookieValue(sh))) == 1
}

func (s *Server) handlePublicUnlock(w http.ResponseWriter, r *http.Request) error {
	sh, err := s.publicShare(r)
	if err != nil {
		return err
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if sh.PasswordHash == "" {
		writeJSON(w, r, 200, map[string]any{"ok": true})
		return nil
	}
	if !s.shareUnlock.Allow(sh.TokenHash) {
		return errorf(http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later")
	}
	if !s.loginSem.TryAcquire() {
		return errorf(http.StatusTooManyRequests, "busy", "server busy; try again")
	}
	defer s.loginSem.Release()
	if len(in.Password) > auth.MaxPasswordLen || !auth.VerifyPassword(sh.PasswordHash, in.Password) {
		s.audit(r, nil, "share.unlock.fail", map[string]any{"id": sh.ID})
		return errorf(http.StatusUnauthorized, "bad_credentials", "wrong password")
	}
	http.SetCookie(w, &http.Cookie{Name: shareCookieName(sh), Value: shareCookieValue(sh), Path: "/", HttpOnly: true, Secure: s.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}

func (s *Server) handlePublicInfo(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	out := map[string]any{"name": sh.Name, "kind": sh.Kind, "expiresAt": sh.ExpiresAt, "now": time.Now().Unix(), "locked": !shareUnlocked(r, sh)}
	if out["locked"] == false {
		s.db.TouchShare(r.Context(), sh.ID)
		if sh.Kind == "file" {
			if e, err := root.Stat(shareFile(sh)); err == nil {
				out["size"] = e.Size
				out["mtime"] = e.Mtime
				out["fileName"] = e.Name
			}
		}
	}
	writeJSON(w, r, 200, out)
	return nil
}

func (s *Server) handlePublicList(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	if !shareUnlocked(r, sh) {
		return errShareLocked
	}
	if sh.Kind == "file" {
		return vfs.ErrNotDir
	}
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	l, err := root.List(p)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"path": p, "entries": l.Entries})
	return nil
}

func (s *Server) acquirePublicDL(r *http.Request) (func(), error) {
	ip := ipFrom(r)
	if !s.publicDL.TryAcquire(ip) {
		return nil, errorf(http.StatusTooManyRequests, "busy", "too many concurrent downloads")
	}
	return func() { s.publicDL.Release(ip) }, nil
}

func (s *Server) handlePublicContent(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	if !shareUnlocked(r, sh) {
		return errShareLocked
	}
	release, err := s.acquirePublicDL(r)
	if err != nil {
		return err
	}
	defer release()
	p := shareFile(sh) // link de arquivo: sempre o próprio arquivo, ?path= ignorado
	if p == "" {
		if p, err = queryPath(r, "path"); err != nil {
			return err
		}
	}
	return serveFile(w, r, root, p, queryBool(r, "inline"))
}

func (s *Server) handlePublicZip(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	if !shareUnlocked(r, sh) {
		return errShareLocked
	}
	if sh.Kind == "file" {
		return vfs.ErrNotDir
	}
	release, err := s.acquirePublicDL(r)
	if err != nil {
		return err
	}
	defer release()
	var paths []string
	for _, raw := range r.URL.Query()["path"] {
		p, err := vfs.NormalizeWritable(raw) // recusa .filezam-* também na leitura
		if err != nil {
			return err
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		paths = []string{""}
	}
	name := ""
	if len(paths) == 1 && paths[0] == "" {
		name = sh.Name
	}
	return serveZip(w, r, root, paths, name)
}
