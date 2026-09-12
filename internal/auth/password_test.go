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
