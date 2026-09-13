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
	"net/http"
	"strconv"
	"time"

	"github.com/miguelzamberlan/filezam/internal/store"
)

// Notificações. O servidor grava o que aconteceu (kind + dados) e a interface monta o texto no
// idioma de quem lê. Caminhos vão base-relativos no banco e saem relativos ao escopo de quem
// consulta, como em favoritos e links; o que ficou fora do escopo sai sem caminho.

const (
	notifyDropReceived = "drop.received" // arquivos chegaram por um link de recebimento
	notifyShareRevoked = "share.revoked" // a manutenção revogou um link cujo item sumiu

	// notificationsKeep é quanto uma notificação fica guardada depois da última atualização.
	notificationsKeep = 30 * 24 * time.Hour
	// dropNamesKept limita os nomes guardados numa notificação agrupada: é um aperitivo, a lista
	// inteira está na pasta.
	dropNamesKept = 5
)

// notifyDrop tells the link's owner that a file arrived. Enquanto a notificação não é lida, os
// envios seguintes pelo mesmo link se somam a ela.
func (s *Server) notifyDrop(ctx context.Context, sh *store.Share, name string, size int64) {
	err := s.db.Notify(ctx, sh.CreatedBy, notifyDropReceived, "drop:"+strconv.FormatInt(sh.ID, 10), func(old map[string]any) map[string]any {
		files, _ := old["files"].(float64)
		bytes, _ := old["bytes"].(float64)
		names := []any{}
		if prev, ok := old["names"].([]any); ok {
			names = prev
		}
		names = append([]any{name}, names...)
		if len(names) > dropNamesKept {
			names = names[:dropNamesKept]
		}
		return map[string]any{"shareId": sh.ID, "link": sh.Name, "path": sh.Path, "files": files + 1, "bytes": bytes + float64(size), "names": names}
	})
	if err != nil {
		s.log.Warn("notify drop", "share", sh.ID, "err", err)
	}
}

// notifyShareRevoked tells the owner that a link was taken down because its item is gone.
func (s *Server) notifyShareRevoked(ctx context.Context, sh *store.Share) {
	err := s.db.Notify(ctx, sh.CreatedBy, notifyShareRevoked, "", func(map[string]any) map[string]any {
		return map[string]any{"shareId": sh.ID, "link": sh.Name, "path": sh.Path, "slug": sh.Slug, "mode": sh.Mode, "reason": "missing"}
	})
	if err != nil {
		s.log.Warn("notify share revoked", "share", sh.ID, "err", err)
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	list, err := s.db.ListNotifications(r.Context(), u.ID, limit)
	if err != nil {
		return err
	}
	for i := range list {
		if p, ok := list[i].Data["path"].(string); ok {
			if rel, inScope := scopeRel(u.Scope, p); inScope {
				list[i].Data["path"] = rel
			} else {
				delete(list[i].Data, "path")
			}
		}
	}
	unread, err := s.db.CountUnreadNotifications(r.Context(), u.ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"notifications": list, "unread": unread})
	return nil
}

func (s *Server) handleNotificationsUnread(w http.ResponseWriter, r *http.Request) error {
	n, err := s.db.CountUnreadNotifications(r.Context(), userFrom(r).ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"unread": n})
	return nil
}

func (s *Server) handleNotificationsRead(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !in.All && len(in.IDs) == 0 {
		return errorf(http.StatusBadRequest, "bad_id", "ids or all required")
	}
	if len(in.IDs) > 1000 {
		return errorf(http.StatusBadRequest, "too_many", "too many ids")
	}
	ids := in.IDs
	if in.All {
		ids = nil
	}
	if err := s.db.MarkNotificationsRead(r.Context(), u.ID, ids); err != nil {
		return err
	}
	return s.handleNotificationsUnread(w, r)
}

func (s *Server) handleNotificationDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return errorf(http.StatusBadRequest, "bad_id", "invalid id")
	}
	if err := s.db.DeleteNotification(r.Context(), userFrom(r).ID, id); err != nil {
		return err
	}
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
