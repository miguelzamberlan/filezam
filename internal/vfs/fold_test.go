// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package vfs

import "testing"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Relatorio.PDF":                       "relatorio.pdf",
		"Relatório de AÇÃO.pdf":               "relatorio de acao.pdf",
		"Relato\u0301rio de Ac\u0327a\u0303o": "relatorio de acao", // NFD, como o macOS grava
		"Crème Brûlée – Ñandú":                "creme brulee – nandu",
		"Straße ẞ":                            "strasse ss",
		"Øresund Łódź Æsir Œuvre":             "oresund lodz aesir oeuvre",
		"İstanbul":                            "istanbul",
		"ﬁcha ２０２６":                           "ficha 2026",
		"日本語.txt":                             "日本語.txt",
		"":                                    "",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}
