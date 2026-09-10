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

export function formatDuration(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '—'
  if (sec < 60) return Math.round(sec) + 's'
  if (sec < 3600) return Math.floor(sec / 60) + 'min ' + Math.round(sec % 60) + 's'
  const h = Math.floor(sec / 3600)
  return h + 'h ' + Math.floor((sec % 3600) / 60) + 'min'
}

const dtf = new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' })

export function formatDate(msOrSec: number, isSec = false): string {
  if (!msOrSec) return '—'
  return dtf.format(new Date(isSec ? msOrSec * 1000 : msOrSec))
}

export function formatRelative(sec: number): string {
  const diff = sec - Math.floor(Date.now() / 1000)
  const abs = Math.abs(diff)
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
