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
