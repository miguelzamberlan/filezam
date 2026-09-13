// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Espelho de vfs.Fold (internal/vfs/fold.go): forma usada para comparar nomes sem diferenciar
// maiúsculas, acentos e cedilha ("Relatório de AÇÃO" → "relatorio de acao"). O servidor
// pesquisa com a versão em Go; esta serve ao filtro da pasta e à digitação rápida.
const special: Record<string, string> = { ß: 'ss', æ: 'ae', œ: 'oe', ø: 'o', đ: 'd', ð: 'd', ł: 'l', ı: 'i', þ: 'th', ħ: 'h' }

export function fold(s: string): string {
  return s
    .normalize('NFKD')
    .replace(/\p{Mn}/gu, '')
    .toLowerCase()
    .replace(/[ßæœøđðłıþħ]/g, (c) => special[c])
}
