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
	"encoding/json"
	"net/http"
	"time"

	"github.com/miguelzamberlan/filezam/internal/store"
)

// audit records an audit event. Best effort; failures are logged.
func (s *Server) audit(r *http.Request, u *store.User, action string, detail map[string]any) {
	e := &store.AuditEntry{TS: time.Now().Unix(), Action: action, IP: ipFrom(r)}
	if u != nil {
		id := u.ID
		e.UserID = &id
		e.Username = u.Username
	}
	if detail != nil {
		b, _ := json.Marshal(detail)
		e.Detail = string(b)
	}
	if err := s.db.AddAudit(context.Background(), e); err != nil {
		s.log.Warn("audit write failed", "err", err)
	}
}
