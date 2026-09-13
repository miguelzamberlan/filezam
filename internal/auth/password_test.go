// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package auth

import (
	"strings"
	"testing"
)

func TestNewReadablePassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		pw, err := NewReadablePassword()
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckPolicy(pw); err != nil {
			t.Fatalf("%q: %v", pw, err)
		}
		if seen[pw] {
			t.Fatalf("repeated password after %d draws: %q", i, pw)
		}
		seen[pw] = true
		// Forma "xxxx-xxxx-xxxx-xxxx", só com o alfabeto sem caracteres ambíguos: ela vai ser lida
		// de um log e digitada à mão.
		groups := strings.Split(pw, "-")
		if len(groups) != 4 {
			t.Fatalf("%q: want 4 groups", pw)
		}
		for _, g := range groups {
			if len(g) != 4 {
				t.Fatalf("%q: group %q is not 4 characters", pw, g)
			}
			for _, r := range g {
				if !strings.ContainsRune(readableAlphabet, r) {
					t.Fatalf("%q: %q is outside the readable alphabet", pw, r)
				}
			}
		}
	}
}
