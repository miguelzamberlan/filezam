// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { backoffMs, chunkRange, classify, pickBatch } from './scheduler'

const l = { chunkSize: 16 << 20, batchFileMax: 1 << 20, batchMaxFiles: 3, batchMaxBytes: 2 << 20 }

describe('scheduler', () => {
  it('classify', () => {
    expect(classify(10, l)).toBe('batch')
    expect(classify(2 << 20, l)).toBe('single')
    expect(classify(17 << 20, l)).toBe('chunked')
  })
  it('pickBatch groups by destDir/overwrite and respects limits', () => {
    const q = [
      { destDir: 'a', size: 100, overwrite: false },
      { destDir: 'b', size: 100, overwrite: false },
      { destDir: 'a', size: 100, overwrite: true },
      { destDir: 'a', size: 1 << 20, overwrite: false },
      { destDir: 'a', size: 1 << 20, overwrite: false },
      { destDir: 'a', size: 100, overwrite: false },
    ]
    const b = pickBatch(q, l)
    expect(b).toEqual([q[0], q[3]])
  })
  it('backoff and chunkRange', () => {
    expect(backoffMs(1)).toBe(1000)
    expect(backoffMs(3)).toBe(4000)
    expect(backoffMs(9)).toBe(16000)
    expect(chunkRange(2, 10, 25)).toEqual([20, 25])
  })
})
