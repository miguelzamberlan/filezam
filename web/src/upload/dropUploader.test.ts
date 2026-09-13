// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { ApiError, isRetryable } from '../api/client'
import { backoffMs, chunkRange } from './scheduler'
import { dropMode, isFatal } from './dropUploader'

describe('dropUploader', () => {
  it('picks the transfer mode at the chunk boundary', () => {
    const chunk = 16 << 20
    expect(dropMode(0, chunk)).toBe('single')
    expect(dropMode(chunk, chunk)).toBe('single')
    expect(dropMode(chunk + 1, chunk)).toBe('chunked')
  })

  it('plans every chunk, last one short', () => {
    const chunk = 4
    const size = 10
    const ranges = [0, 1, 2].map((i) => chunkRange(i, chunk, size))
    expect(ranges).toEqual([[0, 4], [4, 8], [8, 10]])
    expect(ranges.reduce((n, [a, b]) => n + (b - a), 0)).toBe(size)
  })

  // Estourar a cota do link ou o teto por arquivo é uma resposta definitiva: reenviar só
  // gastaria banda do visitante e do servidor para receber o mesmo erro.
  it('never retries a limit imposed by the link', () => {
    for (const code of ['drop_full', 'drop_file_limit', 'drop_count_exceeded', 'share_locked']) {
      expect(isFatal(code)).toBe(true)
    }
    expect(isFatal('busy')).toBe(false)
    expect(isRetryable(new ApiError(429, 'busy', 'busy'))).toBe(true)
    expect(isRetryable(new ApiError(507, 'drop_full', 'full'))).toBe(false)
  })

  it('backs off between attempts', () => {
    expect(backoffMs(1)).toBe(1000)
    expect(backoffMs(6)).toBe(16000)
  })
})
