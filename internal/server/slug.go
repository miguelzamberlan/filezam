package server

import (
	"net/http"
	"strings"
)

// slugMin/slugMax bound a custom link address. O teto de 63 fica abaixo do tamanho do token
// (43 caracteres base64url) não por acaso: a resolução separa os dois pela forma, e um
// token_hash tem sempre 64 hex.
const (
	slugMin = 3
	slugMax = 63
)

// slugReserved é curta de propósito. O apelido vive sob /s/, que é uma rota única da SPA, então
// /s/metrics ou /s/.well-known não colidem com nada do servidor. O que resta é anti-phishing:
// endereços que um visitante leria como "área oficial do sistema".
var slugReserved = map[string]bool{
	"admin": true, "login": true, "filezam": true, "s": true, "api": true,
}

// slugShape reports whether tok looks like a custom address: 3–63 caracteres, minúsculas,
// dígitos e hífen, sem hífen nas pontas. Serve tanto para validar na criação quanto para
// decidir se vale consultar a tabela por apelido.
func slugShape(tok string) bool {
	if len(tok) < slugMin || len(tok) > slugMax {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(tok)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// checkSlug validates a slug typed by a user. Não normaliza: um apelido só existe em forma
// canônica (minúsculas), e aceitar variações abriria duas grafias para o mesmo endereço.
func checkSlug(slug string) error {
	if !slugShape(slug) {
		return errorf(http.StatusBadRequest, "invalid_slug", "the address must have %d-%d characters: lowercase letters, digits and hyphens, not starting or ending with a hyphen", slugMin, slugMax)
	}
	if slugReserved[slug] {
		return errorf(http.StatusBadRequest, "invalid_slug", "this address is reserved")
	}
	return nil
}

// errSlugTaken is the answer for an address already claimed, revoked links included.
var errSlugTaken = errorf(http.StatusConflict, "slug_taken", "this address is already in use")

// normalizeSlug trims what the interface may have sent with spaces around it.
func normalizeSlug(s string) string { return strings.TrimSpace(s) }
