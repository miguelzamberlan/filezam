// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, it, expect } from 'vitest'
import { formatBytes, formatSpeed, formatRelative } from './format'

describe('formatSpeed', () => {
  it('sempre usa duas casas decimais, só a unidade muda', () => {
    expect(formatSpeed(0)).toBe('0.00 B/s')
    expect(formatSpeed(847.3921834781)).toBe('847.39 B/s')
    expect(formatSpeed(1024)).toBe('1.00 KB/s')
    expect(formatSpeed(1536 * 1024)).toBe('1.50 MB/s')
    expect(formatSpeed(3.25 * 1024 ** 3)).toBe('3.25 GB/s')
  })

  it('trata valores inválidos', () => {
    expect(formatSpeed(NaN)).toBe('—')
    expect(formatSpeed(-1)).toBe('—')
  })
})

describe('formatBytes', () => {
  it('não imprime fração de byte', () => {
    expect(formatBytes(847.39)).toBe('847 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
  })
})

describe('formatRelative', () => {
  it('não arredonda menos de um minuto para "0 min"', () => {
    const now = Math.floor(Date.now() / 1000)
    expect(formatRelative(now - 20)).toBe('há instantes')
    expect(formatRelative(now + 20)).toBe('em instantes')
    expect(formatRelative(now - 5 * 60)).toBe('há 5 min')
  })
})
