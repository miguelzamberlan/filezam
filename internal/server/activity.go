// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"sort"
	"sync"
	"time"
)

// Envios em andamento para o painel do administrador. Fica só em memória: o banco já guarda as
// sessões em blocos (tabela uploads), mas os arquivos pequenos — lote e PUT único, que são a
// maioria numa pasta de fotos — não deixam rastro nenhum enquanto chegam.
//
// Cada usuário (ou link de recebimento) tem uma "rodada": ela começa no primeiro envio depois de
// activityIdle parado e soma arquivos e bytes até a próxima pausa. Uma rodada sem requisição em
// curso e parada há mais que activityIdle some da lista.

const activityIdle = 2 * time.Minute

type activityKey struct {
	userID  int64 // dono dos bytes: quem envia ou, num link de recebimento, quem criou o link
	shareID int64 // 0 = envio autenticado
}

type activityEntry struct {
	since, lastAt time.Time
	files, bytes  int64
	inflight      int
}

type uploadActivity struct {
	mu sync.Mutex
	m  map[activityKey]*activityEntry
}

// activityView is one row of "sending now".
type activityView struct {
	UserID   int64 `json:"userId"`
	ShareID  int64 `json:"shareId,omitempty"`
	Files    int64 `json:"files"`
	Bytes    int64 `json:"bytes"`
	Since    int64 `json:"since"`
	LastAt   int64 `json:"lastAt"`
	Inflight int   `json:"inflight"`
}

// entry returns the round for k, starting a new one after a pause (caller holds mu).
func (a *uploadActivity) entry(k activityKey, now time.Time) *activityEntry {
	if a.m == nil {
		a.m = map[activityKey]*activityEntry{}
	}
	e := a.m[k]
	if e == nil || (e.inflight == 0 && now.Sub(e.lastAt) > activityIdle) {
		n := &activityEntry{since: now, lastAt: now}
		if e != nil {
			n.inflight = e.inflight
		}
		a.m[k] = n
		e = n
	}
	return e
}

// begin marks a transfer in progress; the returned func ends it.
func (a *uploadActivity) begin(userID, shareID int64) func() {
	k := activityKey{userID, shareID}
	now := time.Now()
	a.mu.Lock()
	e := a.entry(k, now)
	e.inflight++
	e.lastAt = now
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			if e := a.m[k]; e != nil {
				if e.inflight > 0 {
					e.inflight--
				}
				e.lastAt = time.Now()
			}
			a.mu.Unlock()
		})
	}
}

// add counts what arrived: bytes as they are written, files when they are complete.
func (a *uploadActivity) add(userID, shareID, files, bytes int64) {
	if files == 0 && bytes == 0 {
		return
	}
	now := time.Now()
	a.mu.Lock()
	e := a.entry(activityKey{userID, shareID}, now)
	e.files += files
	e.bytes += bytes
	e.lastAt = now
	a.mu.Unlock()
}

// snapshot lists the live rounds, most recent first, and forgets the finished ones.
func (a *uploadActivity) snapshot() []activityView {
	now := time.Now()
	a.mu.Lock()
	out := []activityView{}
	for k, e := range a.m {
		if e.inflight == 0 && now.Sub(e.lastAt) > activityIdle {
			delete(a.m, k)
			continue
		}
		out = append(out, activityView{UserID: k.userID, ShareID: k.shareID, Files: e.files, Bytes: e.bytes,
			Since: e.since.Unix(), LastAt: e.lastAt.Unix(), Inflight: e.inflight})
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt > out[j].LastAt })
	return out
}
