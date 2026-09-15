// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"testing"
	"time"
)

func TestUploadActivityRounds(t *testing.T) {
	var a uploadActivity
	end := a.begin(7, 0)
	a.add(7, 0, 2, 100)
	if v := a.snapshot(); len(v) != 1 || v[0].Inflight != 1 || v[0].Files != 2 || v[0].Bytes != 100 {
		t.Fatalf("in flight: %+v", v)
	}
	end()
	end() // idempotente
	k := activityKey{7, 0}
	// uma requisição em curso segura a rodada mesmo parada há muito tempo
	stop := a.begin(7, 0)
	a.m[k].lastAt = time.Now().Add(-time.Hour)
	if v := a.snapshot(); len(v) != 1 || v[0].Files != 2 {
		t.Fatalf("inflight round dropped: %+v", v)
	}
	stop()
	// parada além da pausa: some da lista, e o próximo envio começa uma rodada nova
	a.m[k].lastAt = time.Now().Add(-activityIdle - time.Second)
	if v := a.snapshot(); len(v) != 0 {
		t.Fatalf("idle round kept: %+v", v)
	}
	a.add(7, 0, 1, 10)
	a.m[k].lastAt = time.Now().Add(-activityIdle - time.Second)
	a.add(7, 0, 1, 5)
	if v := a.snapshot(); len(v) != 1 || v[0].Files != 1 || v[0].Bytes != 5 {
		t.Fatalf("new round after a pause: %+v", v)
	}
	// o link de recebimento é uma rodada à parte do dono
	a.add(7, 3, 1, 1)
	if v := a.snapshot(); len(v) != 2 {
		t.Fatalf("drop link round: %+v", v)
	}
}
