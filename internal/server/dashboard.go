// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"net/http"
	"time"

	"github.com/miguelzamberlan/filezam/internal/jobs"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/uploads"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Painel do administrador: um retrato do que acontece agora, montado a cada requisição a partir
// do que o servidor já tem (banco, índice, operações e envios em memória). Só leitura; a única
// ação é encerrar uma sessão.

// sessionActiveWindow: a sessão só grava o último acesso a cada 5 min (session.go), então
// "ativo agora" é quem apareceu nos últimos 10.
const sessionActiveWindow = 10 * time.Minute

type dashUser struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Scope      string `json:"scope"`
	Disabled   bool   `json:"disabled"`
	Locked     bool   `json:"locked"`
	TOTP       bool   `json:"totp"`
	Quota      int64  `json:"quota"`
	Used       *int64 `json:"used"` // null enquanto o índice não está pronto (medir varrendo o disco a cada atualização pesaria demais)
	Sessions   int    `json:"sessions"`
	LastSeenAt *int64 `json:"lastSeenAt"`
	Shares     int    `json:"shares"`     // links de leitura no ar
	DropShares int    `json:"dropShares"` // links de recebimento no ar
}

type dashSession struct {
	ID         string `json:"id"`
	UserID     int64  `json:"userId"`
	IP         string `json:"ip"`
	UserAgent  string `json:"userAgent"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

type dashUpload struct {
	UserID    int64  `json:"userId"`
	ShareID   int64  `json:"shareId,omitempty"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Received  int64  `json:"received"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type dashJob struct {
	ID         string `json:"id"`
	UserID     int64  `json:"userId"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	StartedAt  int64  `json:"startedAt"`
}

type dashShare struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	OwnerID int64  `json:"ownerId"`
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := userFrom(r)
	now := time.Now()
	rel := func(p string) string { // mesmo critério da lista de links: relativo ao escopo do admin quando cabe nele
		if x, ok := scopeRel(me.Scope, p); ok {
			return x
		}
		return p
	}

	users, err := s.db.ListUsers(ctx)
	if err != nil {
		return err
	}
	sessions, err := s.db.ListLiveSessions(ctx)
	if err != nil {
		return err
	}
	shares, err := s.db.ListShares(ctx, 0)
	if err != nil {
		return err
	}
	ups, err := s.db.ListOpenUploads(ctx)
	if err != nil {
		return err
	}

	byID := map[int64]*dashUser{}
	outUsers := make([]*dashUser, 0, len(users))
	indexReady := s.indexer != nil && s.indexer.Ready()
	for _, u := range users {
		du := &dashUser{ID: u.ID, Username: u.Username, Role: u.Role, Scope: u.Scope, Disabled: u.Disabled,
			Locked: u.LockedUntil != nil && *u.LockedUntil > now.Unix(), TOTP: u.TOTPEnabled(), Quota: u.Quota}
		if indexReady {
			if used, err := s.usage(ctx, u); err == nil {
				du.Used = &used
			}
		}
		byID[u.ID] = du
		outUsers = append(outUsers, du)
	}

	cur := sessionFrom(r)
	outSessions := make([]dashSession, 0, len(sessions))
	active := map[int64]bool{}
	for _, se := range sessions {
		if du := byID[se.UserID]; du != nil {
			du.Sessions++
			if du.LastSeenAt == nil || se.LastSeenAt > *du.LastSeenAt {
				t := se.LastSeenAt
				du.LastSeenAt = &t
			}
		}
		if now.Unix()-se.LastSeenAt <= int64(sessionActiveWindow.Seconds()) {
			active[se.UserID] = true
		}
		outSessions = append(outSessions, dashSession{ID: se.ID, UserID: se.UserID, IP: se.IP, UserAgent: se.UserAgent,
			CreatedAt: se.CreatedAt, LastSeenAt: se.LastSeenAt, Current: cur != nil && cur.ID == se.ID})
	}

	type shareTotals struct {
		Read     int `json:"read"`
		Drop     int `json:"drop"`
		Broken   int `json:"broken"`
		Expiring int `json:"expiring"` // no ar e vencendo nos próximos 3 dias
		Created7 int `json:"created7d"`
	}
	var st shareTotals
	shareByID := map[int64]*store.Share{}
	for _, sh := range shares {
		shareByID[sh.ID] = sh
		if sh.CreatedAt >= now.Add(-7*24*time.Hour).Unix() {
			st.Created7++
		}
		if sh.RevokedAt != nil || sh.ExpiresAt <= now.Unix() {
			continue
		}
		if sh.Mode == "drop" {
			st.Drop++
		} else {
			st.Read++
		}
		if du := byID[sh.CreatedBy]; du != nil {
			if sh.Mode == "drop" {
				du.DropShares++
			} else {
				du.Shares++
			}
		}
		if sh.BrokenSince != nil {
			st.Broken++
		}
		if sh.ExpiresAt-now.Unix() <= 3*86400 {
			st.Expiring++
		}
	}

	// Links citados em "em andamento": o painel mostra o nome, sem que o front precise da lista toda.
	shareRefs := map[int64]dashShare{}
	refShare := func(id int64) {
		if sh := shareByID[id]; id != 0 && sh != nil {
			shareRefs[id] = dashShare{ID: sh.ID, Name: sh.Name, Mode: sh.Mode, OwnerID: sh.CreatedBy}
		}
	}

	outUploads := make([]dashUpload, 0, len(ups))
	for _, up := range ups {
		outUploads = append(outUploads, dashUpload{UserID: up.UserID, ShareID: up.ShareID, Path: rel(vfs.Join(up.Dir, up.Name)), Size: up.Size,
			Received: uploads.ReceivedBytes(up), CreatedAt: up.CreatedAt, UpdatedAt: up.UpdatedAt})
		refShare(up.ShareID)
	}
	activity := s.activity.snapshot()
	for _, a := range activity {
		refShare(a.ShareID)
	}
	outJobs := []dashJob{}
	for _, j := range s.jobs.RunningAll() {
		outJobs = append(outJobs, dashJobView(j))
	}
	refs := make([]dashShare, 0, len(shareRefs))
	for _, v := range shareRefs {
		refs = append(refs, v)
	}

	day := now.Add(-24 * time.Hour).Unix()
	logins, err := s.db.CountAuditSince(ctx, day, "login.ok", "login.fail", "totp.fail", "login.locked")
	if err != nil {
		return err
	}
	drops, err := s.db.DropTotalsSince(ctx, now.Add(-7*24*time.Hour).Unix())
	if err != nil {
		return err
	}

	out := map[string]any{
		"now":         now.Unix(),
		"activeUsers": len(active),
		"users":       outUsers,
		"sessions":    outSessions,
		"activity":    activity,
		"uploads":     outUploads,
		"jobs":        outJobs,
		"shareRefs":   refs,
		"shares":      st,
		"logins24h":   map[string]int64{"ok": logins["login.ok"], "failed": logins["login.fail"] + logins["totp.fail"], "locked": logins["login.locked"]},
		"drops7d":     map[string]int64{"files": drops.Count, "bytes": drops.Bytes},
	}
	d := s.base.Disk()
	out["disk"] = map[string]any{"total": d.Total, "free": d.Free}
	if items, err := s.db.ListTrash(ctx, 0); err == nil {
		var bytes int64
		for _, it := range items {
			bytes += it.Size
		}
		out["trash"] = map[string]int64{"items": int64(len(items)), "bytes": bytes}
	}
	idx := map[string]any{"enabled": s.indexer != nil}
	if s.indexer != nil {
		is := s.indexer.Status(ctx)
		idx["ready"], idx["running"], idx["lastFullAt"] = is.Ready, is.Running, is.LastFullAt
	}
	out["index"] = idx
	writeJSON(w, r, 200, out)
	return nil
}

func dashJobView(j jobs.OwnedView) dashJob {
	return dashJob{ID: j.ID, UserID: j.UserID, Type: j.Type, Label: j.Label, Done: j.Done, Total: j.Total,
		BytesDone: j.BytesDone, BytesTotal: j.BytesTotal, StartedAt: j.StartedAt}
}

// DELETE /api/admin/sessions/{id}: encerra a sessão de alguém. A própria sessão fica de fora
// (para isso existe o Sair), e o evento vai para a auditoria com o dono da sessão.
func (s *Server) handleAdminSessionRevoke(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if cur := sessionFrom(r); cur != nil && cur.ID == id {
		return errorf(http.StatusConflict, "self", "use logout to end your own session")
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if err := s.db.DeleteSession(ctx, sess.ID); err != nil {
		return err
	}
	detail := map[string]any{"userId": sess.UserID, "ip": sess.IP}
	if u, err := s.db.GetUser(ctx, sess.UserID); err == nil {
		detail["username"] = u.Username
	}
	s.audit(r, userFrom(r), "session.revoke", detail)
	writeJSON(w, r, 200, map[string]any{"ok": true})
	return nil
}
