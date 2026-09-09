package server

import (
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

// shareRoot resolves the token to a live share and opens its root.
func (s *Server) shareRoot(r *http.Request) (*vfs.Root, *store.Share, error) {
	if !s.publicIP.Allow(ipFrom(r)) {
		return nil, nil, errorf(http.StatusTooManyRequests, "rate_limited", "too many requests")
	}
	tok := r.PathValue("token")
	if len(tok) < 16 || len(tok) > 128 {
		return nil, nil, errShareNotFound
	}
	sh, err := s.db.GetActiveShareByToken(r.Context(), auth.HashToken(tok))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errShareNotFound
		}
		return nil, nil, err
	}
	root, err := s.base.Sub(sh.Path)
	if err != nil {
		s.log.Warn("share folder unavailable", "id", sh.ID, "path", sh.Path, "err", err)
		return nil, nil, errShareNotFound
	}
	if e, err := root.Stat(""); err != nil || e.Type != "dir" {
		root.Close()
		return nil, nil, errShareNotFound
	}
	return root, sh, nil
}

func (s *Server) handlePublicInfo(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	root.Close()
	s.db.TouchShare(r.Context(), sh.ID)
	writeJSON(w, r, 200, map[string]any{"name": sh.Name, "expiresAt": sh.ExpiresAt, "now": time.Now().Unix()})
	return nil
}

func (s *Server) handlePublicList(w http.ResponseWriter, r *http.Request) error {
	root, _, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
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
	root, _, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := s.acquirePublicDL(r)
	if err != nil {
		return err
	}
	defer release()
	p, err := queryPath(r, "path")
	if err != nil {
		return err
	}
	return serveFile(w, r, root, p, queryBool(r, "inline"))
}

func (s *Server) handlePublicZip(w http.ResponseWriter, r *http.Request) error {
	root, sh, err := s.shareRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := s.acquirePublicDL(r)
	if err != nil {
		return err
	}
	defer release()
	var paths []string
	for _, raw := range r.URL.Query()["path"] {
		p, err := vfs.Normalize(raw)
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
