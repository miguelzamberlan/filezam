// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Alertas do painel do admin, calculados no cliente a partir do retrato do servidor. Os textos
// ficam na página (S.alert*); aqui só a regra de quando cada um aparece.
import type { Dashboard } from '../api/types'

export type AlertLevel = 'danger' | 'warn' | 'info'

export type DashAlert =
  | { kind: 'disk'; level: AlertLevel; pct: number }
  | { kind: 'quota'; level: AlertLevel; user: string; pct: number }
  | { kind: 'broken'; level: AlertLevel; count: number }
  | { kind: 'loginFails'; level: AlertLevel; count: number }
  | { kind: 'locked'; level: AlertLevel; user: string }
  | { kind: 'staleUploads'; level: AlertLevel; count: number }
  | { kind: 'indexBuilding'; level: AlertLevel }
  | { kind: 'indexOff'; level: AlertLevel }

export const DISK_WARN = 80
export const DISK_DANGER = 90
export const QUOTA_DANGER = 90
export const STALE_UPLOAD_SEC = 3600 // sessões em blocos sem bloco novo há 1 h (são descartadas em 24 h)

const order: Record<AlertLevel, number> = { danger: 0, warn: 1, info: 2 }

export function usagePct(used: number, total: number): number {
  return total > 0 ? Math.round((used / total) * 100) : 0
}

export function dashboardAlerts(d: Dashboard): DashAlert[] {
  const out: DashAlert[] = []
  const disk = usagePct(d.disk.total - d.disk.free, d.disk.total)
  if (disk >= DISK_DANGER) out.push({ kind: 'disk', level: 'danger', pct: disk })
  else if (disk >= DISK_WARN) out.push({ kind: 'disk', level: 'warn', pct: disk })
  for (const u of d.users) {
    if (u.disabled) continue
    if (u.quota > 0 && u.used !== null) {
      const pct = usagePct(u.used, u.quota)
      if (pct >= QUOTA_DANGER) out.push({ kind: 'quota', level: 'danger', user: u.username, pct })
    }
    if (u.locked) out.push({ kind: 'locked', level: 'warn', user: u.username })
  }
  if (d.shares.broken > 0) out.push({ kind: 'broken', level: 'warn', count: d.shares.broken })
  if (d.logins24h.failed > 0) out.push({ kind: 'loginFails', level: 'warn', count: d.logins24h.failed })
  const stale = d.uploads.filter((u) => d.now - u.updatedAt > STALE_UPLOAD_SEC).length
  if (stale > 0) out.push({ kind: 'staleUploads', level: 'info', count: stale })
  // uso por usuário só importa a quem tem cota; sem ninguém com cota, o aviso seria ruído
  if (d.users.some((u) => u.quota > 0)) {
    if (!d.index.enabled) out.push({ kind: 'indexOff', level: 'info' })
    else if (!d.index.ready) out.push({ kind: 'indexBuilding', level: 'info' })
  }
  return out.sort((a, b) => order[a.level] - order[b.level])
}
