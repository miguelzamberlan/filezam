// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import type { Dashboard, DashUser } from '../api/types'
import { dashboardAlerts } from './dashboard'

const user = (o: Partial<DashUser>): DashUser => ({
  id: 1, username: 'ana', role: 'user', scope: '', disabled: false, locked: false, totp: false, quota: 0, used: 0, sessions: 0, lastSeenAt: null, shares: 0, dropShares: 0, ...o,
})

const base = (o: Partial<Dashboard> = {}): Dashboard => ({
  now: 10_000, activeUsers: 0, users: [], sessions: [], activity: [], uploads: [], jobs: [], shareRefs: [],
  shares: { read: 0, drop: 0, broken: 0, expiring: 0, created7d: 0 }, logins24h: { ok: 0, failed: 0, locked: 0 }, drops7d: { files: 0, bytes: 0 },
  disk: { total: 100, free: 50 }, index: { enabled: true, ready: true }, ...o,
})

describe('dashboardAlerts', () => {
  it('servidor tranquilo não gera alerta', () => {
    expect(dashboardAlerts(base({ users: [user({ quota: 100, used: 50 })] }))).toEqual([])
  })

  it('disco: aviso a partir de 80 %, perigo a partir de 90 %', () => {
    expect(dashboardAlerts(base({ disk: { total: 100, free: 20 } }))).toEqual([{ kind: 'disk', level: 'warn', pct: 80 }])
    expect(dashboardAlerts(base({ disk: { total: 100, free: 5 } }))).toEqual([{ kind: 'disk', level: 'danger', pct: 95 }])
  })

  it('perigo vem antes de aviso e de informação', () => {
    const d = base({
      users: [user({ username: 'bia', quota: 100, used: 95 }), user({ username: 'caio', locked: true }), user({ username: 'off', disabled: true, quota: 10, used: 10 })],
      shares: { read: 1, drop: 0, broken: 2, expiring: 0, created7d: 0 },
      logins24h: { ok: 1, failed: 3, locked: 0 },
      uploads: [{ userId: 1, path: 'a', size: 10, received: 1, createdAt: 0, updatedAt: 1000 }, { userId: 1, path: 'b', size: 10, received: 1, createdAt: 0, updatedAt: 9_000 }],
    })
    expect(dashboardAlerts(d)).toEqual([
      { kind: 'quota', level: 'danger', user: 'bia', pct: 95 },
      { kind: 'locked', level: 'warn', user: 'caio' },
      { kind: 'broken', level: 'warn', count: 2 },
      { kind: 'loginFails', level: 'warn', count: 3 },
      { kind: 'staleUploads', level: 'info', count: 1 },
    ])
  })

  it('índice fora do ar só avisa quando alguém tem cota', () => {
    expect(dashboardAlerts(base({ index: { enabled: false } }))).toEqual([])
    expect(dashboardAlerts(base({ index: { enabled: false }, users: [user({ quota: 100, used: null })] }))).toEqual([{ kind: 'indexOff', level: 'info' }])
    expect(dashboardAlerts(base({ index: { enabled: true, ready: false }, users: [user({ quota: 100, used: null })] }))).toEqual([{ kind: 'indexBuilding', level: 'info' }])
  })
})
