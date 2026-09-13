// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { fold } from './fold'

describe('fold', () => {
  it('ignora maiúsculas, acentos e cedilha como o servidor', () => {
    expect(fold('Relatório de AÇÃO.pdf')).toBe('relatorio de acao.pdf')
    expect(fold('Relato\u0301rio de Ac\u0327a\u0303o')).toBe('relatorio de acao') // NFD do macOS
    expect(fold('Crème Brûlée – Ñandú')).toBe('creme brulee – nandu')
    expect(fold('Straße ẞ')).toBe('strasse ss')
    expect(fold('Øresund Łódź Æsir Œuvre')).toBe('oresund lodz aesir oeuvre')
    expect(fold('İstanbul')).toBe('istanbul')
    expect(fold('ﬁcha ２０２６')).toBe('ficha 2026')
    expect(fold('日本語.txt')).toBe('日本語.txt')
  })
})
