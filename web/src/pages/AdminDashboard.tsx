// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useMemo, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { NavLink } from 'react-router'
import { Api, ApiError } from '../api/client'
import type { Dashboard, DashSession, DashUser } from '../api/types'
import { dashboardAlerts, usagePct, DISK_DANGER, DISK_WARN, QUOTA_DANGER, STALE_UPLOAD_SEC, type DashAlert } from '../lib/dashboard'
import { formatBytes, formatDate, formatRelative } from '../lib/format'
import { jobLabel } from '../lib/jobs'
import { describeUserAgent } from '../lib/userAgent'
import { S, errorMessage } from '../strings'
import { dialogs, toast } from '../components/dialogs'
import { IAlert, IRefresh, ISpinner } from '../components/Icons'
import { MenuButton } from '../components/Shell'

// Painel do administrador: o que acontece agora no servidor. Atualiza sozinho enquanto a aba
// está visível (o react-query não busca em segundo plano).
const REFRESH_MS = 15_000
const ACTIVE_SEC = 600 // mesmo critério do servidor (sessionActiveWindow)

function Bar({ pct, className = '' }: { pct: number; className?: string }) {
  const color = pct >= DISK_DANGER ? 'bg-red-500' : pct >= DISK_WARN ? 'bg-amber-500' : 'bg-accent'
  return (
    <div className={'h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800 ' + className}>
      <div className={'h-full ' + color} style={{ width: Math.min(100, pct) + '%' }} />
    </div>
  )
}

function Stat({ label, value, sub, children }: { label: string; value: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="card min-w-0 px-3 py-2.5">
      <div className="truncate text-xs text-neutral-500">{label}</div>
      <div className="mt-0.5 text-xl font-semibold tabular-nums">{value}</div>
      {children}
      {sub && <div className="mt-0.5 text-xs text-neutral-500">{sub}</div>}
    </div>
  )
}

function Section({ title, children, right }: { title: string; children: ReactNode; right?: ReactNode }) {
  return (
    <section className="mt-6">
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-neutral-500">{title}</h2>
        {right && <div className="ml-auto">{right}</div>}
      </div>
      {children}
    </section>
  )
}

const th = 'px-3 py-2 font-medium'
const td = 'px-3 py-1.5'
const row = 'border-t border-neutral-200 dark:border-neutral-800'

function alertText(a: DashAlert): string {
  switch (a.kind) {
    case 'disk': return S.alertDisk(a.pct)
    case 'quota': return S.alertQuota(a.user, a.pct)
    case 'broken': return S.alertBroken(a.count)
    case 'loginFails': return S.alertLoginFails(a.count)
    case 'locked': return S.alertLocked(a.user)
    case 'staleUploads': return S.alertStaleUploads(a.count)
    case 'indexBuilding': return S.alertIndexBuilding
    case 'indexOff': return S.alertIndexOff
  }
}

function lastSeen(u: DashUser, now: number) {
  if (u.lastSeenAt === null) return <span className="text-neutral-400">—</span>
  if (now - u.lastSeenAt <= ACTIVE_SEC) return <span className="inline-flex items-center gap-1.5 text-emerald-600"><span className="h-2 w-2 rounded-full bg-emerald-500" />{S.dashActiveNow}</span>
  return <span title={formatDate(u.lastSeenAt, true)}>{formatRelative(u.lastSeenAt)}</span>
}

function Usage({ u }: { u: DashUser }) {
  if (u.used === null) return <span className="text-neutral-400">{u.quota > 0 ? '— / ' + formatBytes(u.quota, 0) : '—'}</span>
  if (u.quota <= 0) return <span>{formatBytes(u.used)} <span className="text-xs text-neutral-400">· {S.dashNoQuota}</span></span>
  const pct = usagePct(u.used, u.quota)
  return (
    <div className="min-w-32">
      <div className="flex justify-between gap-2 text-xs tabular-nums">
        <span>{formatBytes(u.used)} / {formatBytes(u.quota, 0)}</span>
        <span className={pct >= QUOTA_DANGER ? 'text-red-600' : 'text-neutral-500'}>{pct}%</span>
      </div>
      <Bar pct={pct} className="mt-1" />
    </div>
  )
}

export default function AdminDashboard() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['admin-dashboard'], queryFn: () => Api.adminDashboard(), refetchInterval: REFRESH_MS })
  const d = q.data
  const alerts = useMemo(() => (d ? dashboardAlerts(d) : []), [d])
  const names = useMemo(() => new Map((d?.users ?? []).map((u) => [u.id, u.username])), [d])
  const shareNames = useMemo(() => new Map((d?.shareRefs ?? []).map((s) => [s.id, s])), [d])
  const name = (id: number) => names.get(id) ?? '#' + id
  // envio por link de recebimento aparece em nome do link, com o dono entre parênteses
  const who = (userId: number, shareId?: number) => {
    const sh = shareId ? shareNames.get(shareId) : undefined
    return shareId ? S.dashDropBy(sh?.name ?? '#' + shareId, name(userId)) : name(userId)
  }

  const endSession = async (s: DashSession) => {
    const device = describeUserAgent(s.userAgent)
    if (!(await dialogs.confirm({ title: S.dashEndSessionTitle, message: S.dashEndSessionConfirm(name(s.userId), device), danger: true, okLabel: S.dashEndSession }))) return
    try {
      await Api.adminRevokeSession(s.id)
      toast(S.dashSessionEnded, 'success')
    } catch (e) {
      toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')
    }
    qc.invalidateQueries({ queryKey: ['admin-dashboard'] })
  }

  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-1 flex items-center gap-2">
        <MenuButton />
        <h1 className="text-lg font-semibold">{S.dashboard}</h1>
        <button className="btn-ghost ml-auto" onClick={() => q.refetch()} disabled={q.isFetching} title={S.refresh}>
          {q.isFetching ? <ISpinner /> : <IRefresh size={16} />}
        </button>
      </div>
      <p className="mb-4 text-xs text-neutral-500">{S.dashHint}</p>
      {q.isLoading && <ISpinner />}
      {q.error && <p className="text-sm text-red-600">{q.error instanceof ApiError ? errorMessage(q.error.code, q.error.message) : String(q.error)}</p>}
      {d && <DashboardBody d={d} alerts={alerts} who={who} name={name} endSession={endSession} />}
    </div>
  )
}

function DashboardBody({ d, alerts, who, name, endSession }: {
  d: Dashboard
  alerts: DashAlert[]
  who: (userId: number, shareId?: number) => string
  name: (id: number) => string
  endSession: (s: DashSession) => void
}) {
  const diskPct = usagePct(d.disk.total - d.disk.free, d.disk.total)
  const sendingBytes = d.activity.reduce((n, a) => n + a.bytes, 0)
  const users = [...d.users].sort((a, b) => (b.lastSeenAt ?? -1) - (a.lastSeenAt ?? -1) || a.username.localeCompare(b.username))
  const nothingRunning = d.activity.length === 0 && d.uploads.length === 0 && d.jobs.length === 0

  return (
    <>
      <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-6">
        <Stat label={S.dashActiveUsers} value={d.activeUsers} sub={S.dashActiveUsersSub(d.users.length, d.sessions.length)} />
        <Stat label={S.dashSending} value={d.activity.length} sub={S.dashSendingSub(formatBytes(sendingBytes), d.uploads.length)} />
        <Stat label={S.dashJobs} value={d.jobs.length} sub={S.dashJobsSub} />
        <Stat label={S.dashLinks} value={d.shares.read + d.shares.drop} sub={S.dashLinksSub(d.shares.drop, d.shares.expiring)} />
        <Stat label={S.disk} value={diskPct + '%'} sub={S.diskFree(formatBytes(d.disk.free), formatBytes(d.disk.total))}>
          <Bar pct={diskPct} className="mt-1" />
        </Stat>
        <Stat label={S.trash} value={d.trash?.items ?? '—'} sub={d.trash ? formatBytes(d.trash.bytes) : undefined} />
      </div>

      {alerts.length > 0 && (
        <Section title={S.dashAlerts}>
          <ul className="card divide-y divide-neutral-200 text-sm dark:divide-neutral-800">
            {alerts.map((a, i) => (
              <li key={i} className="flex items-start gap-2 px-3 py-2">
                <IAlert size={16} className={'mt-0.5 shrink-0 ' + (a.level === 'danger' ? 'text-red-600' : a.level === 'warn' ? 'text-amber-600' : 'text-neutral-400')} />
                <span className="min-w-0 flex-1">{alertText(a)}</span>
                {(a.kind === 'loginFails' || a.kind === 'locked') && <NavLink to="/admin/audit" className="shrink-0 text-xs text-accent hover:underline">{S.dashSeeAudit}</NavLink>}
                {a.kind === 'broken' && <NavLink to="/shares" className="shrink-0 text-xs text-accent hover:underline">{S.shares}</NavLink>}
              </li>
            ))}
          </ul>
        </Section>
      )}

      <Section title={S.dashNow}>
        {nothingRunning && <p className="card px-3 py-3 text-sm text-neutral-500">{S.dashNothing}</p>}
        {d.activity.length > 0 && (
          <div className="card mb-2 overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase text-neutral-500">
                <tr><th className={th}>{S.dashUploadsRounds}</th><th className={th}>{S.dashReceived}</th><th className={'hidden sm:table-cell ' + th}>{S.jobStarted}</th><th className={th}>{S.dashLastActivity}</th></tr>
              </thead>
              <tbody>
                {d.activity.map((a) => (
                  <tr key={a.userId + ':' + (a.shareId ?? 0)} className={row}>
                    <td className={td + ' font-medium'}>
                      <span className="inline-flex items-center gap-1.5">
                        <span className={'h-2 w-2 shrink-0 rounded-full ' + (a.inflight > 0 ? 'animate-pulse bg-emerald-500' : 'bg-neutral-300 dark:bg-neutral-600')} title={a.inflight > 0 ? S.dashStateSending : S.dashStateIdle} />
                        {who(a.userId, a.shareId)}
                      </span>
                    </td>
                    <td className={td + ' whitespace-nowrap tabular-nums'}>{S.dashFilesBytes(a.files, formatBytes(a.bytes))}</td>
                    <td className={'hidden whitespace-nowrap sm:table-cell ' + td}>{formatRelative(a.since)}</td>
                    <td className={td + ' whitespace-nowrap'}>{a.inflight > 0 ? <span className="text-emerald-600">{S.dashStateSending}</span> : formatRelative(a.lastAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {d.uploads.length > 0 && (
          <div className="card mb-2 overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase text-neutral-500">
                <tr><th className={th}>{S.dashChunked}</th><th className={'hidden md:table-cell ' + th}>{S.dashWho}</th><th className={th}>{S.dashProgress}</th><th className={'hidden sm:table-cell ' + th}>{S.dashLastActivity}</th></tr>
              </thead>
              <tbody>
                {d.uploads.map((u, i) => {
                  const pct = usagePct(u.received, u.size)
                  const stalled = d.now - u.updatedAt > STALE_UPLOAD_SEC
                  return (
                    <tr key={i} className={row}>
                      <td className={td + ' max-w-xs truncate'} title={'/' + u.path}>/{u.path}<div className="text-xs text-neutral-500 md:hidden">{who(u.userId, u.shareId)}</div></td>
                      <td className={'hidden md:table-cell ' + td}>{who(u.userId, u.shareId)}</td>
                      <td className={td}>
                        <div className="min-w-28 text-xs tabular-nums">{formatBytes(u.received)} / {formatBytes(u.size)} · {pct}%</div>
                        <div className="mt-1 h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800"><div className="h-full bg-accent" style={{ width: pct + '%' }} /></div>
                      </td>
                      <td className={'hidden whitespace-nowrap sm:table-cell ' + td}>{formatRelative(u.updatedAt)}{stalled && <span className="ml-1.5 text-xs text-amber-600">{S.dashStateStalled}</span>}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {d.jobs.length > 0 && (
          <div className="card overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase text-neutral-500">
                <tr><th className={th}>{S.jobsTitle}</th><th className={'hidden md:table-cell ' + th}>{S.dashWho}</th><th className={th}>{S.dashProgress}</th><th className={'hidden sm:table-cell ' + th}>{S.jobStarted}</th></tr>
              </thead>
              <tbody>
                {d.jobs.map((j) => (
                  <tr key={j.id} className={row}>
                    <td className={td + ' max-w-xs'}><div className="font-medium">{jobLabel(j.type)}</div><div className="truncate text-xs text-neutral-500" title={j.label}>{j.label}</div></td>
                    <td className={'hidden md:table-cell ' + td}>{name(j.userId)}</td>
                    <td className={td + ' whitespace-nowrap text-xs tabular-nums'}>{S.jobItems(j.done, j.total)}{j.bytesTotal > 0 && ` · ${formatBytes(j.bytesDone)} / ${formatBytes(j.bytesTotal)}`}</td>
                    <td className={'hidden whitespace-nowrap sm:table-cell ' + td}>{formatRelative(j.startedAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>

      <Section title={S.users} right={<NavLink to="/admin/users" className="text-xs text-accent hover:underline">{S.dashManageUsers}</NavLink>}>
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-neutral-500">
              <tr>
                <th className={th}>{S.username}</th>
                <th className={'hidden lg:table-cell ' + th}>{S.scope}</th>
                <th className={th}>{S.dashLastSeen}</th>
                <th className={'hidden sm:table-cell ' + th}>{S.dashSessions}</th>
                <th className={th}>{S.dashUsage}</th>
                <th className={'hidden md:table-cell ' + th}>{S.dashLinksCol}</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} className={row + (u.disabled ? ' opacity-50' : '')}>
                  <td className={td + ' font-medium'}>
                    {u.username}
                    {u.role === 'admin' && <span className="ml-2 rounded bg-neutral-200 px-1.5 text-xs font-normal dark:bg-neutral-800">{S.roleAdmin}</span>}
                    {u.disabled && <span className="ml-2 rounded bg-neutral-200 px-1.5 text-xs font-normal dark:bg-neutral-800">{S.disabled}</span>}
                    {u.locked && <span className="ml-2 rounded bg-red-100 px-1.5 text-xs font-normal text-red-800 dark:bg-red-900/50 dark:text-red-200">{S.dashLocked}</span>}
                  </td>
                  <td className={'hidden max-w-48 truncate lg:table-cell ' + td}>{u.scope === '' ? S.scopeRoot : '/' + u.scope}</td>
                  <td className={td + ' whitespace-nowrap'}>{lastSeen(u, d.now)}</td>
                  <td className={'hidden tabular-nums sm:table-cell ' + td}>{u.sessions || <span className="text-neutral-400">0</span>}</td>
                  <td className={td}><Usage u={u} /></td>
                  <td className={'hidden whitespace-nowrap md:table-cell ' + td}>{u.shares + u.dropShares > 0 ? S.dashLinksCell(u.shares, u.dropShares) : <span className="text-neutral-400">—</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>

      <Section title={S.dashSessionsTitle}>
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-neutral-500">
              <tr>
                <th className={th}>{S.username}</th>
                <th className={th}>{S.dashDevice}</th>
                <th className={'hidden md:table-cell ' + th}>{S.ip}</th>
                <th className={'hidden lg:table-cell ' + th}>{S.dashSignedIn}</th>
                <th className={'hidden sm:table-cell ' + th}>{S.dashLastSeen}</th>
                <th className={th}></th>
              </tr>
            </thead>
            <tbody>
              {d.sessions.map((s) => (
                <tr key={s.id} className={row}>
                  <td className={td + ' font-medium'}>{name(s.userId)}</td>
                  <td className={td + ' max-w-48 truncate'} title={s.userAgent}>{describeUserAgent(s.userAgent)}</td>
                  <td className={'hidden font-mono text-xs md:table-cell ' + td}>{s.ip}</td>
                  <td className={'hidden whitespace-nowrap lg:table-cell ' + td}>{formatDate(s.createdAt, true)}</td>
                  <td className={'hidden whitespace-nowrap sm:table-cell ' + td}>
                    {d.now - s.lastSeenAt <= ACTIVE_SEC ? <span className="text-emerald-600">{S.dashActiveNow}</span> : <span title={formatDate(s.lastSeenAt, true)}>{formatRelative(s.lastSeenAt)}</span>}
                  </td>
                  <td className={td + ' text-right'}>
                    {s.current ? <span className="whitespace-nowrap text-xs text-neutral-500">{S.dashThisSession}</span>
                      : <button className="btn-ghost !px-1.5 !py-0.5 text-xs text-red-600" onClick={() => endSession(s)}>{S.dashEndSession}</button>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Section>

      <Section title={S.dashRecent} right={<NavLink to="/admin/audit" className="text-xs text-accent hover:underline">{S.dashSeeAudit}</NavLink>}>
        <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-5">
          <Stat label={S.dashLoginsOk} value={d.logins24h.ok} />
          <Stat label={S.dashLoginsFailed} value={<span className={d.logins24h.failed > 0 ? 'text-amber-600' : ''}>{d.logins24h.failed}</span>} />
          <Stat label={S.dashLockouts} value={<span className={d.logins24h.locked > 0 ? 'text-red-600' : ''}>{d.logins24h.locked}</span>} />
          <Stat label={S.dashLinksCreated} value={d.shares.created7d} />
          <Stat label={S.dashDropsReceived} value={d.drops7d.files} sub={formatBytes(d.drops7d.bytes)} />
        </div>
      </Section>
    </>
  )
}
