// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import type { EntryType } from '../api/types'
import { S } from '../strings'

const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(n: number, digits = 1): string {
  if (!Number.isFinite(n) || n < 0) return '—'
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (i === 0 ? Math.round(v).toString() : v.toFixed(digits)) + ' ' + units[i]
}

// Velocidade sempre com duas casas decimais: só a unidade muda, para o número
// não ficar pulando de largura enquanto o upload varia.
export function formatSpeed(bytesPerSec: number): string {
  if (!Number.isFinite(bytesPerSec) || bytesPerSec < 0) return '—'
  let i = 0
  let v = bytesPerSec
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return v.toFixed(2) + ' ' + units[i] + '/s'
}

/**
 * Duração legível. Acima de um dia o tempo vira "3d 4h", e o que passa de cem dias não é
 * informação nenhuma — é conta com um divisor perto de zero, que sem o teto imprimia o
 * número em notação científica no meio da frase ("2.16e+68h 37min restante").
 */
export function formatDuration(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '—'
  if (sec < 60) return Math.round(sec) + 's'
  if (sec < 3600) return Math.floor(sec / 60) + 'min ' + Math.round(sec % 60) + 's'
  if (sec < 86400) return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'min'
  if (sec >= 100 * 86400) return '—'
  return Math.floor(sec / 86400) + 'd ' + Math.floor((sec % 86400) / 3600) + 'h'
}

const dtf = new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' })

export function formatDate(msOrSec: number, isSec = false): string {
  if (!msOrSec) return '—'
  return dtf.format(new Date(isSec ? msOrSec * 1000 : msOrSec))
}

export function formatRelative(sec: number): string {
  const diff = sec - Math.floor(Date.now() / 1000)
  const abs = Math.abs(diff)
  // Menos de um minuto arredondaria para "há 0 min".
  if (abs < 60) return diff < 0 ? S.relJustNow : S.relSoon
  const s = abs < 3600 ? Math.round(abs / 60) + ' min' : abs < 86400 ? Math.round(abs / 3600) + ' h' : Math.round(abs / 86400) + ' d'
  return diff < 0 ? S.relAgo(s) : S.relIn(s)
}

export function extOf(name: string): string {
  const i = name.lastIndexOf('.')
  return i > 0 ? name.slice(i + 1).toLowerCase() : ''
}

export function splitExt(name: string): [string, string] {
  const i = name.lastIndexOf('.')
  return i > 0 ? [name.slice(0, i), name.slice(i)] : [name, '']
}

/**
 * Rótulo da coluna "Tipo": a extensão em maiúsculas (PDF, JPG), que é o que a ordenação por tipo
 * agrupa. Pasta e arquivo sem extensão ganham palavra; o que não é nem um nem outro (symlink
 * quebrado, dispositivo) não tem tipo a mostrar.
 */
export function typeLabel(name: string, type: EntryType): string {
  if (type === 'dir') return S.folder
  if (type !== 'file') return '—'
  const ext = extOf(name)
  return ext ? ext.toUpperCase() : S.file
}

/**
 * Duração de mídia como relógio: 1:23:45 com horas, 2:05 sem. Diferente de formatDuration,
 * que descreve um tempo decorrido ("2min 5s"); aqui o formato é o que um reprodutor mostra.
 */
export function formatClock(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => n.toString().padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

/** Duração somada de uma pasta: "12 h 34 min" lê melhor que 12:34:56 quando passa de horas. */
export function formatSpan(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  if (h === 0 && m === 0) return `${total} s`
  if (h === 0) return `${m} min`
  return `${h} h ${m} min`
}

/** Taxa de bits em múltiplos decimais, como os fabricantes anunciam (8,9 Mb/s). */
export function formatBitrate(bps: number): string {
  if (!Number.isFinite(bps) || bps <= 0) return '—'
  if (bps >= 1e9) return (bps / 1e9).toFixed(1) + ' Gb/s'
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mb/s'
  if (bps >= 1e3) return Math.round(bps / 1e3) + ' kb/s'
  return bps + ' b/s'
}

/** Megapixels de um par de dimensões, com uma casa quando o número é pequeno. */
export function formatMegapixels(pixels: number): string {
  if (!Number.isFinite(pixels) || pixels <= 0) return '—'
  const mp = pixels / 1e6
  return mp >= 100 ? Math.round(mp).toString() : mp.toFixed(1)
}

const utcDtf = new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short', timeZone: 'UTC' })

/**
 * Data de captura. O EXIF e o cabeçalho de vídeo gravam a hora sem fuso nenhum, e o servidor
 * a converte lendo-a como UTC: mostrar em UTC devolve o relógio que estava no equipamento,
 * em vez de deslocá-lo pelo fuso de quem está olhando.
 */
export function formatCaptureDate(ms: number): string {
  if (!ms) return '—'
  return utcDtf.format(new Date(ms))
}
