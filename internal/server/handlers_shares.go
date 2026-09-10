package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

type shareView struct {
	ID           int64  `json:"id"`
	Token        string `json:"token"` // vazio em links criados antes da migração 002
	Path         string `json:"path"`
	Name         string `json:"name"`
	CreatedBy    string `json:"createdBy"`
	Mine         bool   `json:"mine"`
	CreatedAt    int64  `json:"createdAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	Expired      bool   `json:"expired"`
	AccessCount  int64  `json:"accessCount"`
	LastAccessAt *int64 `json:"lastAccessAt"`
	Kind         string `json:"kind"`        // "dir" | "file"
	HasPassword  bool   `json:"hasPassword"` // nunca o hash
}

func (s *Server) viewShare(sh *store.Share, u *store.User) shareView {
	p := sh.Path
	if rel, ok := scopeRel(u.Scope, sh.Path); ok {
		p = rel
	}
	return shareView{ID: sh.ID, Token: sh.Token, Path: p, Name: sh.Name, CreatedBy: sh.CreatedByName, Mine: sh.CreatedBy == u.ID, CreatedAt: sh.CreatedAt,
		ExpiresAt: sh.ExpiresAt, Expired: sh.ExpiresAt <= time.Now().Unix() || sh.RevokedAt != nil, AccessCount: sh.AccessCount, LastAccessAt: sh.LastAccessAt, Kind: sh.Kind, HasPassword: sh.PasswordHash != ""}
}

func (s *Server) shareURL(r *http.Request, token string) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL + "/s/" + token
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.isTrustedRequest(r) {
		host = fh
	}
	return scheme + "://" + host + "/s/" + token
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	uid := u.ID
	if u.IsAdmin() {
		uid = 0
	}
	shares, err := s.db.ListShares(r.Context(), uid)
	if err != nil {
		return err
	}
	out := make([]shareView, 0, len(shares))
	for _, sh := range shares {
		if sh.RevokedAt != nil {
			continue
		}
		out = append(out, s.viewShare(sh, u))
	}
	writeJSON(w, r, 200, map[string]any{"shares": out, "now": time.Now().Unix()})
	return nil
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	var in struct {
		Path      string `json:"path"`
		ExpiresIn int64  `json:"expiresIn"`
		Name      string `json:"name"`
		Password  string `json:"password"` // opcional; 4–256 caracteres
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := vfs.Normalize(in.Path)
	if err != nil {
		return err
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	if e.Type != "dir" && e.Type != "file" {
		return vfs.ErrNotDir
	}
	if p == "" && e.Type == "file" {
		return vfs.ErrNotDir
	}
	pwHash := ""
	if in.Password != "" {
		if n := len([]rune(in.Password)); n < 4 || n > 256 {
			return errorf(http.StatusBadRequest, "weak_password", "share password must have 4-256 characters")
		}
		if pwHash, err = auth.HashPassword(in.Password); err != nil {
			return err
		}
	}
	if in.ExpiresIn <= 0 || in.ExpiresIn > int64(s.cfg.ShareMaxTTL.Seconds()) {
		return errorf(http.StatusBadRequest, "bad_expiry", "expiresIn must be between 1 and %d seconds", int64(s.cfg.ShareMaxTTL.Seconds()))
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = vfs.Base(p)
		if name == "" {
			name = "Arquivos"
		}
	}
	if len(name) > 100 {
		name = name[:100]
	}
	tok, err := auth.NewToken(32)
	if err != nil {
		return err
	}
	dev, ino, _ := root.Identity(p) // zeros quando o sistema de arquivos não informa
	now := time.Now().Unix()
	sh, err := s.db.CreateShare(r.Context(), &store.Share{TokenHash: auth.HashToken(tok), Token: tok, Kind: e.Type, PasswordHash: pwHash, Dev: dev, Ino: ino, Path: vfs.Join(u.Scope, p), Name: name, CreatedBy: u.ID, CreatedAt: now, ExpiresAt: now + in.ExpiresIn})
	if err != nil {
		return err
	}
	s.audit(r, u, "share.create", map[string]any{"id": sh.ID, "path": sh.Path, "kind": sh.Kind, "password": pwHash != "", "expiresAt": sh.ExpiresAt})
	writeJSON(w, r, 201, map[string]any{"share": s.viewShare(sh, u), "token": tok, "url": s.shareURL(r, tok)})
	return nil
}

func (s *Server) handleShareDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	u := userFrom(r)
	sh, err := s.db.GetShare(r.Context(), id)
	if err != nil {
		return err
	}
	if sh.CreatedBy != u.ID && !u.IsAdmin() {
		return errForbidden
	}
	if err := s.db.DeleteShare(r.Context(), id); err != nil {
		return err
	}
	s.audit(r, u, "share.revoke", map[string]any{"id": id, "path": sh.Path})
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
