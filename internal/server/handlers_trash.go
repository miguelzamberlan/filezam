package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Lixeira: excluir move o item para <escopo do usuário>/.filezam-trash/<id>/<nome> e grava uma
// linha com o caminho original. Restaurar devolve ao lugar (com nome único se já houver algo);
// esvaziar/expirar apaga de vez. Tudo com caminhos base-relativos sobre s.base.

type trashView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"` // caminho original, relativo ao escopo de quem consulta
	Type      string `json:"type"`
	Size      int64  `json:"size"`
	DeletedAt int64  `json:"deletedAt"`
	By        string `json:"by"`
}

// trashOne moves one scope-relative path into the user's trash and records it.
func (s *Server) trashOne(ctx context.Context, root *vfs.Root, u *store.User, p string, e *vfs.Entry) error {
	id, err := auth.NewID(16)
	if err != nil {
		return err
	}
	// A linha vem antes do movimento: uma queda no meio deixa, no pior caso, uma linha sem
	// item (restaurar responde not_found e a apaga; a retenção também a limpa), nunca um item
	// escondido na lixeira sem linha — esse ficaria invisível e fora da cota para sempre.
	item := &store.TrashItem{ID: id, UserID: u.ID, TrashDir: vfs.Join(u.Scope, vfs.TrashDirName), Name: vfs.Base(p), Path: vfs.Join(u.Scope, p), Type: e.Type, Size: e.Size, DeletedAt: time.Now().Unix()}
	if err := s.db.AddTrash(context.Background(), item); err != nil {
		return err
	}
	if err := root.MoveToTrash(ctx, p, vfs.TrashDirName, id); err != nil {
		// Nada chegou à lixeira: desfaz a linha. Se algo chegou (cópia entre dispositivos
		// interrompida), a linha fica para o item continuar visível e restaurável.
		if _, serr := root.StatReserved(vfs.Join(vfs.TrashDirName, id), item.Name); serr != nil {
			_ = root.RemoveTrashItem(context.Background(), vfs.TrashDirName, id)
			_ = s.db.DeleteTrash(context.Background(), id)
		}
		return err
	}
	s.indexRemove(item.Path)
	return nil
}

// visibleTrash lists the rows the caller may act on: their own, or every row whose original
// path lies inside the caller's scope when the caller is an admin.
func (s *Server) visibleTrash(ctx context.Context, u *store.User) ([]*store.TrashItem, error) {
	uid := u.ID
	if u.IsAdmin() {
		uid = 0
	}
	items, err := s.db.ListTrash(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if _, ok := scopeRel(u.Scope, it.Path); ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Server) handleTrashList(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	items, err := s.visibleTrash(r.Context(), u)
	if err != nil {
		return err
	}
	views := make([]trashView, 0, len(items))
	for _, it := range items {
		rel, _ := scopeRel(u.Scope, it.Path)
		views = append(views, trashView{ID: it.ID, Name: it.Name, Path: rel, Type: it.Type, Size: it.Size, DeletedAt: it.DeletedAt, By: it.UserName})
	}
	writeJSON(w, r, 200, map[string]any{"items": views, "retention": int64(s.cfg.TrashRetention.Seconds())})
	return nil
}

func readIDs(r *http.Request) ([]string, error) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	if len(in.IDs) == 0 || len(in.IDs) > 1000 {
		return nil, errorf(http.StatusBadRequest, "bad_id", "ids must have 1-1000 entries")
	}
	return in.IDs, nil
}

// pickVisible returns the visible rows matching ids, in request order.
func (s *Server) pickVisible(ctx context.Context, u *store.User, ids []string) ([]*store.TrashItem, error) {
	items, err := s.visibleTrash(ctx, u)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.TrashItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	var out []*store.TrashItem
	for _, id := range ids {
		if it := byID[id]; it != nil {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	ids, err := readIDs(r)
	if err != nil {
		return err
	}
	items, err := s.pickVisible(r.Context(), u, ids)
	if err != nil {
		return err
	}
	type res struct {
		ID   string `json:"id"`
		Path string `json:"path,omitempty"`
		Code string `json:"code,omitempty"`
	}
	restored, failed := []res{}, []res{}
	dirs := map[string]bool{}
	for _, it := range items {
		dst := it.Path
		dir := vfs.Dir(dst)
		if err := s.base.MkdirAll(dir); err != nil {
			failed = append(failed, res{ID: it.ID, Code: toAPIError(err).Code})
			continue
		}
		if ok, _ := s.base.Exists(dst); ok {
			name, err := s.base.UniqueName(dir, it.Name)
			if err != nil {
				failed = append(failed, res{ID: it.ID, Code: toAPIError(err).Code})
				continue
			}
			dst = vfs.Join(dir, name)
		}
		if err := s.base.RestoreFromTrash(r.Context(), it.TrashDir, it.ID, it.Name, dst); err != nil {
			if errors.Is(err, vfs.ErrNotFound) {
				_ = s.db.DeleteTrash(r.Context(), it.ID) // linha sem item (queda ao excluir): some da lista
			}
			failed = append(failed, res{ID: it.ID, Code: toAPIError(err).Code})
			continue
		}
		_ = s.db.DeleteTrash(r.Context(), it.ID)
		s.indexTree(dst)
		rel, _ := scopeRel(u.Scope, dst)
		restored = append(restored, res{ID: it.ID, Path: rel})
		dirs[dir] = true
	}
	s.audit(r, u, "trash.restore", map[string]any{"restored": len(restored), "failed": len(failed)})
	writeJSON(w, r, 200, map[string]any{"restored": restored, "failed": failed})
	return nil
}

func (s *Server) purgeTrash(ctx context.Context, items []*store.TrashItem) int {
	n := 0
	for _, it := range items {
		if err := s.base.RemoveTrashItem(ctx, it.TrashDir, it.ID); err != nil && !errors.Is(err, vfs.ErrNotFound) {
			s.log.Warn("trash purge", "id", it.ID, "err", err)
			continue
		}
		if err := s.db.DeleteTrash(ctx, it.ID); err == nil {
			n++
		}
	}
	return n
}

func (s *Server) handleTrashDelete(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	ids, err := readIDs(r)
	if err != nil {
		return err
	}
	items, err := s.pickVisible(r.Context(), u, ids)
	if err != nil {
		return err
	}
	n := s.purgeTrash(r.Context(), items)
	s.audit(r, u, "trash.delete", map[string]any{"deleted": n})
	writeJSON(w, r, 200, map[string]any{"deleted": n})
	return nil
}

func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	items, err := s.visibleTrash(r.Context(), u)
	if err != nil {
		return err
	}
	n := s.purgeTrash(r.Context(), items)
	s.audit(r, u, "trash.empty", map[string]any{"deleted": n})
	writeJSON(w, r, 200, map[string]any{"deleted": n})
	return nil
}

// sweepTrash permanently removes items older than the retention (runs hourly).
func (s *Server) sweepTrash(ctx context.Context) {
	if s.cfg.TrashRetention <= 0 {
		return
	}
	items, err := s.db.ListTrashBefore(ctx, s.db.Now().Add(-s.cfg.TrashRetention).Unix())
	if err != nil {
		s.log.Warn("trash sweep", "err", err)
		return
	}
	if n := s.purgeTrash(ctx, items); n > 0 {
		s.log.Info("trash sweep", "removed", n)
	}
}
