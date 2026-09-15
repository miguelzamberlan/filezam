// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

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
	// A linha vem antes do movimento, pendente: nunca fica item escondido na lixeira sem linha
	// (invisível e fora da cota para sempre). Se o processo cair no meio, recoverPendingTrash
	// retoma a exclusão a partir dela.
	item := &store.TrashItem{ID: id, UserID: u.ID, TrashDir: vfs.Join(u.Scope, vfs.TrashDirName), Name: vfs.Base(p), Path: vfs.Join(u.Scope, p), Type: e.Type, Size: e.Size, DeletedAt: time.Now().Unix(), Pending: true}
	bg := context.Background()
	if err := s.db.AddTrash(bg, item); err != nil {
		return err
	}
	if err := root.MoveToTrash(ctx, p, vfs.TrashDirName, id); err != nil {
		if errors.Is(err, vfs.ErrSourceNotRemoved) {
			// O item já está inteiro na lixeira; o que sobrou na origem é o resto de uma remoção
			// que falhou. A linha fica, concluída, para ele seguir restaurável.
			_ = s.db.FinishTrash(bg, id)
			s.indexTree(item.Path)
			return err
		}
		// A cópia não terminou, então a origem está inteira: a lixeira parcial e a linha somem.
		_ = root.RemoveTrashItem(bg, vfs.TrashDirName, id)
		_ = s.db.DeleteTrash(bg, id)
		return err
	}
	if err := s.db.FinishTrash(bg, id); err != nil {
		s.log.Warn("trash finish", "id", id, "err", err)
	}
	s.indexRemove(item.Path)
	return nil
}

// recoverPendingTrash resumes deletions that a crash cut short. Só olha linhas de antes deste
// processo subir: as mais novas podem estar em andamento agora.
//
//   - origem sumiu e o item está na lixeira: o movimento terminou; limpa temporários e conclui.
//   - origem existe: a exclusão foi pedida e não terminou; retoma (copia o que falta pulando o que
//     já chegou inteiro, depois remove a origem).
//   - nenhum dos dois: linha sem item. Apaga a linha — mas só se a pasta da lixeira existe. O perigo
//     é um item escondido na lixeira sem linha, e ele só pode estar numa lixeira que não se vê
//     (disco não montado); com a lixeira à vista e sem o item, não há nada escondido em lugar
//     nenhum. Sem ela, espera a próxima rodada.
func (s *Server) recoverPendingTrash(ctx context.Context) {
	items, err := s.db.ListPendingTrash(ctx, s.started.Unix())
	if err != nil {
		s.log.Warn("trash recovery", "err", err)
		return
	}
	exists := func(p string) bool {
		if p == "" {
			return true
		}
		_, err := s.base.StatReserved(vfs.Dir(p), vfs.Base(p))
		return err == nil
	}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return
		}
		inTrash := exists(vfs.Join(it.TrashDir, it.ID, it.Name))
		switch {
		case exists(it.Path):
			s.dropShares(it.Path)
			if err := s.base.ResumeMoveToTrash(ctx, it.Path, it.TrashDir, it.ID); err != nil {
				s.log.Warn("trash recovery: resume", "id", it.ID, "path", it.Path, "err", err)
				continue
			}
			s.indexRemove(it.Path)
			s.log.Info("trash recovery: deletion finished", "id", it.ID, "path", it.Path)
		case inTrash:
			if _, err := s.base.RemoveTemps(ctx, vfs.Join(it.TrashDir, it.ID, it.Name), it.ID); err != nil {
				s.log.Warn("trash recovery: temps", "id", it.ID, "err", err)
				continue
			}
		case exists(it.TrashDir):
			_ = s.base.RemoveTrashItem(ctx, it.TrashDir, it.ID)
			if err := s.db.DeleteTrash(ctx, it.ID); err != nil {
				s.log.Warn("trash recovery: ghost row", "id", it.ID, "err", err)
			}
			continue
		default:
			continue // disco não montado
		}
		if err := s.db.FinishTrash(ctx, it.ID); err != nil {
			s.log.Warn("trash recovery: finish", "id", it.ID, "err", err)
		}
	}
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
	writeJSON(w, r, 200, map[string]any{"items": views, "retention": int64(s.trashRetention().Seconds())})
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
		dir, err := s.base.MkdirAll(dir)
		if err != nil {
			failed = append(failed, res{ID: it.ID, Code: toAPIError(err).Code})
			continue
		}
		name, err := s.base.UniqueIfExists(dir, it.Name)
		if err != nil {
			failed = append(failed, res{ID: it.ID, Code: toAPIError(err).Code})
			continue
		}
		dst = vfs.Join(dir, name)
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
	retention := s.trashRetention()
	if retention <= 0 {
		return
	}
	items, err := s.db.ListTrashBefore(ctx, s.db.Now().Add(-retention).Unix())
	if err != nil {
		s.log.Warn("trash sweep", "err", err)
		return
	}
	if n := s.purgeTrash(ctx, items); n > 0 {
		s.log.Info("trash sweep", "removed", n)
	}
}
