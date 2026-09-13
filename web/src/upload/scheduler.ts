// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

/** Pure scheduling helpers for the upload manager (unit-tested). */

export type Mode = 'batch' | 'single' | 'chunked'

export interface Limits {
  chunkSize: number
  batchFileMax: number
  batchMaxFiles: number
  batchMaxBytes: number
}

export function classify(size: number, l: Limits): Mode {
  if (size <= l.batchFileMax) return 'batch'
  if (size <= l.chunkSize) return 'single'
  return 'chunked'
}

export interface Batchable {
  destDir: string
  size: number
  overwrite: boolean
}

/** Pick a group of small files sharing destination and overwrite flag, bounded by count and bytes. */
export function pickBatch<T extends Batchable>(queued: T[], l: Limits): T[] {
  const first = queued[0]
  if (!first) return []
  const out: T[] = []
  let bytes = 0
  for (const it of queued) {
    if (it.destDir !== first.destDir || it.overwrite !== first.overwrite) continue
    if (out.length >= l.batchMaxFiles || (out.length > 0 && bytes + it.size > l.batchMaxBytes)) break
    out.push(it)
    bytes += it.size
  }
  return out
}

export function backoffMs(attempt: number): number {
  return Math.min(16000, 1000 * 2 ** Math.max(0, attempt - 1))
}

export function chunkRange(index: number, chunkSize: number, size: number): [number, number] {
  const start = index * chunkSize
  return [start, Math.min(start + chunkSize, size)]
}
