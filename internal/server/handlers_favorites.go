package server

import (
	"net/http"
	"strings"

	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// scopeRel converts a base-relative path into a scope-relative one.
func scopeRel(scope, p string) (string, bool) {
	if scope == "" {
		return p, true
	}
	if p == scope {
		return "", true
	}
	if strings.HasPrefix(p, scope+"/") {
		return p[len(scope)+1:], true
	}
	return "", false
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	favs, err := s.db.ListFavorites(r.Context(), u.ID)
	if err != nil {
		return err
	}
	out := make([]store.Favorite, 0, len(favs))
	for _, f := range favs {
		rel, ok := scopeRel(u.Scope, f.Path)
		if !ok {
			continue
		}
		f.Path = rel
		out = append(out, f)
	}
	writeJSON(w, r, 200, map[string]any{"favorites": out})
	return nil
}

func (s *Server) handleFavoriteAdd(w http.ResponseWriter, r *http.Request) error {
	root, u, err := s.userRoot(r)
	if err != nil {
		return err
	}
	defer root.Close()
	var in struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := vfs.NormalizeWritable(in.Path)
	if err != nil {
		return err
	}
	e, err := root.Stat(p)
	if err != nil {
		return err
	}
	if e.Type != "dir" {
		return vfs.ErrNotDir
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = vfs.Base(p)
		if name == "" {
			name = "Início"
		}
	}
	if len(name) > 100 {
		name = name[:100]
	}
	f, err := s.db.AddFavorite(r.Context(), u.ID, vfs.Join(u.Scope, p), name)
	if err != nil {
		return err
	}
	f.Path = p
	writeJSON(w, r, 201, map[string]any{"favorite": f})
	return nil
}

func (s *Server) handleFavoriteDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.db.DeleteFavorite(r.Context(), userFrom(r).ID, id); err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
