// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package vfs

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FoldVersion muda sempre que Fold passar a devolver outra coisa para algum nome. O índice
// guarda nomes já dobrados e é redobrado na abertura do banco quando a versão gravada fica
// para trás; sem subir a versão, as linhas antigas deixariam de casar com o termo novo.
const FoldVersion = 1

// foldSpecial cobre letras que não se decompõem em base + acento no Unicode.
var foldSpecial = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ð': "d", 'ł': "l", 'ı': "i", 'þ': "th", 'ħ': "h",
}

// Fold reduz um nome à forma usada para comparar na pesquisa: sem diferenciar maiúsculas,
// sem acentos nem cedilha ("Relatório de AÇÃO" → "relatorio de acao") e com as formas de
// compatibilidade abertas (ligadura "ﬁ" → "fi", dígitos largos → ASCII). A decomposição
// NFKD também iguala nomes gravados em NFD (macOS) aos gravados em NFC.
func Fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if x, ok := foldSpecial[r]; ok {
			b.WriteString(x)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
