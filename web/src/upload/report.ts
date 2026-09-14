// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Relatório dos envios que não chegaram ao destino (falhos e cancelados). Existe só enquanto a
// fila está na memória: é o que a pessoa copia ou baixa para saber o que falta sem conferir a
// pasta arquivo por arquivo.
import { join } from '../lib/paths'
import { S, errorMessage } from '../strings'
import type { UploadItem } from './manager'

type ReportItem = Pick<UploadItem, 'destDir' | 'relPath' | 'size' | 'state' | 'error' | 'errorCode'>

export function isProblem(it: Pick<UploadItem, 'state'>): boolean {
  return it.state === 'failed' || it.state === 'cancelled'
}

export function problemItems<T extends Pick<UploadItem, 'state'>>(items: T[]): T[] {
  return items.filter(isProblem)
}

function reason(it: ReportItem): string {
  return it.state === 'cancelled' ? S.jobCancelled : errorMessage(it.errorCode, it.error)
}

function fullPath(it: ReportItem): string {
  return join(it.destDir, it.relPath)
}

/** Uma linha por item: "caminho — motivo". */
export function problemText(items: ReportItem[]): string {
  return problemItems(items).map((it) => `${fullPath(it)} — ${reason(it)}`).join('\n')
}

function csvField(v: string | number): string {
  const s = String(v)
  return /[";\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

/** CSV separado por ";" com BOM, para o Excel em português abrir com acentos e colunas certas. */
export function problemCsv(items: ReportItem[]): string {
  const h = S.uploadReportColumns
  const rows = [[h.path, h.size, h.state, h.reason]]
  for (const it of problemItems(items)) {
    const cancelled = it.state === 'cancelled'
    rows.push([fullPath(it), String(it.size), cancelled ? S.jobCancelled : S.jobFailed, cancelled ? '' : reason(it)])
  }
  return '﻿' + rows.map((r) => r.map(csvField).join(';')).join('\r\n') + '\r\n'
}

/** Baixa um texto como arquivo, sem passar pelo servidor. */
export function downloadText(text: string, filename: string, type: string) {
  const url = URL.createObjectURL(new Blob([text], { type }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
