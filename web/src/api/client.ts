// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useUpdate } from '../lib/updates'
import type {
  AdminUser, AppConfig, AppNotification, ZipPart, ZipPlan, AuditEntry, Conflict, Dashboard, DiskUsage, Entry, EntryInfo, Favorite, IndexStatus, Job, JobRecord, ListPage, Listing, PublicInfo, SearchResult, Settings, Share, TrashItem, UploadSession, User,
} from './types'

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public extra: Record<string, unknown> = {}) {
    super(message)
  }
}

// 408/'incomplete_body' entram aqui porque são falhas de rede no meio da transferência: tentar de
// novo é exatamente o certo, e antes disso caíam como erro definitivo na tela.
export const isRetryable = (e: unknown) =>
  e instanceof ApiError &&
  (e.status === 429 || e.status === 408 || e.status === 502 || e.status === 503 || e.status === 504 || e.status === 0 || e.code === 'incomplete_body')

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export async function parseError(res: Response): Promise<ApiError> {
  let code = 'internal'
  let message = res.statusText
  let extra: Record<string, unknown> = {}
  try {
    const body = await res.json()
    if (body?.error) {
      const { code: c, message: m, ...rest } = body.error
      code = c ?? code
      message = m ?? message
      extra = rest
    }
  } catch {
    /* non-JSON body */
  }
  return new ApiError(res.status, code, message, extra)
}

export async function api<T>(method: string, url: string, body?: unknown, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { 'X-Filezam': '1', ...(init.headers as Record<string, string>) }
  let payload: BodyInit | undefined
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch(url, { ...init, method, headers, body: payload, credentials: 'same-origin' })
  } catch (e) {
    throw new ApiError(0, 'network', String(e))
  }
  // Antes do tratamento de erro: uma resposta 401 ou 500 também diz qual servidor respondeu, e é
  // justamente depois de uma atualização que a aba antiga tende a esbarrar em alguma delas.
  useUpdate.getState().note(res.headers.get('X-Filezam-Version'))
  if (!res.ok) {
    const err = await parseError(res)
    if (err.status === 401 && onUnauthorized && !url.startsWith('/api/auth/login')) onUnauthorized()
    throw err
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

const q = (params: Record<string, string | number | boolean | undefined>) => {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') sp.set(k, String(v))
  const s = sp.toString()
  return s ? '?' + s : ''
}

// zipQuery monta ?path=…&name=…&from=…&to=… de um zip (ou de uma parte dele).
function zipQuery(paths: string[], name?: string, part?: ZipPart | null): string {
  const sp = new URLSearchParams()
  for (const p of paths) sp.append('path', p)
  if (name) sp.set('name', name)
  if (part) {
    sp.set('from', part.from)
    sp.set('to', part.to)
  }
  return sp.toString()
}

export const Api = {
  // auth
  login: (username: string, password: string) => api<{ user?: User; totpRequired?: boolean; token?: string }>('POST', '/api/auth/login', { username, password }),
  loginTOTP: (token: string, code: string, trust: boolean) => api<{ user: User }>('POST', '/api/auth/totp', { token, code, trust }),
  totpSetup: () => api<{ secret: string; uri: string }>('POST', '/api/auth/totp/setup'),
  totpEnable: (code: string, password: string) => api<{ user: User; recoveryCodes: string[] }>('POST', '/api/auth/totp/enable', { code, password }),
  totpDisable: (password: string, code: string) => api<{ user: User }>('POST', '/api/auth/totp/disable', { password, code }),
  totpRecovery: (password: string, code: string) => api<{ recoveryCodes: string[] }>('POST', '/api/auth/totp/recovery', { password, code }),
  adminTOTPReset: (id: number) => api<{ ok: true }>('POST', `/api/admin/users/${id}/totp/reset`),
  logout: () => api<{ ok: true }>('POST', '/api/auth/logout'),
  me: () => api<{ user: User }>('GET', '/api/auth/me'),
  changePassword: (current: string, next: string) => api<{ user: User }>('POST', '/api/auth/password', { current, new: next }),
  config: () => api<AppConfig>('GET', '/api/config'),

  // files
  list: (path: string, page?: ListPage) =>
    api<Listing>('GET', '/api/files' + q(page ? { path, offset: page.offset, limit: page.limit, sort: page.sort, dir: page.dir, hidden: page.hidden ? 1 : 0 } : { path })),
  // Só as subpastas: o seletor de destino de "Mover para…" não desenha arquivos, e pedir a
  // listagem inteira de uma pasta enorme só para achar as pastas seria um JSON de megabytes.
  dirs: (path: string) => api<{ path: string; dirs: string[] }>('GET', '/api/files/dirs' + q({ path })),
  stat: (path: string) => api<{ path: string; entry: Entry }>('GET', '/api/files/stat' + q({ path })),
  info: (path: string) => api<EntryInfo>('GET', '/api/files/info' + q({ path })),
  disk: () => api<DiskUsage>('GET', '/api/files/disk'),
  search: (path: string, query: string, limit?: number) => api<SearchResult>('GET', '/api/files/search' + q({ path, q: query, limit })),
  // putContent grava texto pequeno (editor). Não passa pelo helper api(): ele serializa o corpo
  // como JSON e fixa o Content-Type. ifMtime faz o servidor recusar se o arquivo mudou desde que
  // foi aberto, em vez de sobrescrever em silêncio.
  putContent: async (path: string, text: string, ifMtime?: number) => {
    const res = await fetch('/api/files/content' + q({ path, overwrite: 1, ifMtime }), {
      method: 'PUT',
      headers: { 'X-Filezam': '1', 'Content-Type': 'application/octet-stream' },
      credentials: 'same-origin',
      body: new Blob([text]),
    }).catch((e) => {
      throw new ApiError(0, 'network', String(e))
    })
    if (!res.ok) throw await parseError(res)
    return (await res.json()) as { path: string; entry: Entry }
  },

  contentUrl: (path: string, inline = false) => '/api/files/content' + q({ path, inline: inline ? 1 : undefined }),
  zipUrl: (paths: string[], name?: string, part?: ZipPart | null) => '/api/files/zip?' + zipQuery(paths, name, part),
  zipPlan: (paths: string[]) => api<ZipPlan>('GET', '/api/files/zip/plan?' + zipQuery(paths)),
  mkdir: (path: string) => api<{ path: string; entry: Entry }>('POST', '/api/files/mkdir', { path }),
  rename: (path: string, newName: string) => api<{ path: string; entry: Entry }>('POST', '/api/files/rename', { path, newName }),
  delete: (paths: string[], permanent = false) => api<{ job: Job }>('POST', '/api/files/delete', permanent ? { paths, permanent } : { paths }),
  copy: (sources: string[], destDir: string, onConflict: Conflict) =>
    api<{ job: Job }>('POST', '/api/files/copy', { sources, destDir, onConflict }),
  move: (sources: string[], destDir: string, onConflict: Conflict) =>
    api<{ job: Job }>('POST', '/api/files/move', { sources, destDir, onConflict }),

  extract: (path: string) => api<{ job: Job }>('POST', '/api/files/extract', { path }),
  archive: (paths: string[], name?: string) => api<{ job: Job }>('POST', '/api/files/archive', { paths, name: name ?? '' }),

  // jobs
  jobs: () => api<{ jobs: Job[] }>('GET', '/api/jobs'),
  jobHistory: (limit = 100) => api<{ jobs: JobRecord[] }>('GET', '/api/jobs/history' + q({ limit })),
  job: (id: string) => api<{ job: Job }>('GET', '/api/jobs/' + id),
  cancelJob: (id: string) => api<{ ok: true }>('DELETE', '/api/jobs/' + id),

  // uploads (chunked sessions)
  uploadCreate: (dir: string, name: string, size: number, mtime: number | null, overwrite: boolean) =>
    api<UploadSession>('POST', '/api/uploads', { dir, name, size, mtime, overwrite }),
  uploadList: () => api<{ uploads: UploadSession[] }>('GET', '/api/uploads'),
  uploadGet: (id: string) => api<UploadSession>('GET', '/api/uploads/' + id),
  uploadComplete: (id: string) => api<{ entry: Entry }>('POST', `/api/uploads/${id}/complete`),
  uploadAbort: (id: string) => api<{ ok: true }>('DELETE', '/api/uploads/' + id),

  // favorites
  favorites: () => api<{ favorites: Favorite[] }>('GET', '/api/favorites'),
  addFavorite: (path: string, name?: string) => api<{ favorite: Favorite }>('POST', '/api/favorites', { path, name: name ?? '' }),
  removeFavorite: (id: number) => api<{ ok: true }>('DELETE', '/api/favorites/' + id),

  // shares
  shares: () => api<{ shares: Share[]; now: number }>('GET', '/api/shares'),
  createShare: (opts: { path: string; expiresIn: number; name?: string; password?: string; slug?: string; mode?: 'read' | 'drop'; quotaBytes?: number; maxFileBytes?: number; maxFiles?: number }) =>
    api<{ share: Share; token: string; url: string }>('POST', '/api/shares', {
      path: opts.path,
      expiresIn: opts.expiresIn,
      name: opts.name ?? '',
      ...(opts.password ? { password: opts.password } : {}),
      ...(opts.slug ? { slug: opts.slug } : {}),
      ...(opts.mode === 'drop' ? { mode: 'drop', quotaBytes: opts.quotaBytes, maxFileBytes: opts.maxFileBytes, maxFiles: opts.maxFiles } : {}),
    }),
  // purge libera o apelido de vez; sem ele um link com apelido é só revogado e o endereço
  // continua reservado a quem o criou.
  sharesAffected: (paths: string[]) => api<{ count: number; links: { path: string; mode: 'read' | 'drop'; slug?: string }[] }>('POST', '/api/shares/affected', { paths }),
  deleteShare: (id: number, purge = false) => api<{ ok: true }>('DELETE', '/api/shares/' + id + (purge ? '?purge=1' : '')),

  // trash
  trash: () => api<{ items: TrashItem[]; retention: number }>('GET', '/api/trash'),
  trashRestore: (ids: string[]) => api<{ restored: { id: string; path: string }[]; failed: { id: string; code: string }[] }>('POST', '/api/trash/restore', { ids }),
  trashDelete: (ids: string[]) => api<{ deleted: number }>('POST', '/api/trash/delete', { ids }),
  trashEmpty: () => api<{ deleted: number }>('POST', '/api/trash/empty'),

  adminSettings: () => api<{ settings: Settings; dropTtlHardMax: number }>('GET', '/api/admin/settings'),
  adminUpdateSettings: (patch: Partial<Settings>) => api<{ settings: Settings; dropTtlHardMax: number }>('PATCH', '/api/admin/settings', patch),

  // notificações
  notifications: (limit = 100) => api<{ notifications: AppNotification[]; unread: number }>('GET', '/api/notifications?limit=' + limit),
  notificationsUnread: () => api<{ unread: number }>('GET', '/api/notifications/unread'),
  markNotificationsRead: (ids: number[] | 'all') => api<{ unread: number }>('POST', '/api/notifications/read', ids === 'all' ? { all: true } : { ids }),
  deleteNotification: (id: number) => api<{ ok: true }>('DELETE', '/api/notifications/' + id),

  // admin index
  adminIndex: () => api<IndexStatus>('GET', '/api/admin/index'),
  adminReindex: () => api<{ started: boolean }>('POST', '/api/admin/reindex'),

  // public
  publicInfo: (token: string) => api<PublicInfo>('GET', '/api/public/' + token),
  publicUnlock: (token: string, password: string) => api<{ ok: true }>('POST', `/api/public/${token}/unlock`, { password }),
  publicList: (token: string, path: string) => api<Listing>('GET', `/api/public/${token}/list` + q({ path })),
  publicContentUrl: (token: string, path: string, inline = false) =>
    `/api/public/${token}/content` + q({ path, inline: inline ? 1 : undefined }),
  // Envio anônimo. Os blocos e o PUT único passam pelo XHR (progresso), não por estas funções.
  dropCreate: (token: string, name: string, size: number, mtime: number | null) =>
    api<UploadSession>('POST', `/api/public/${token}/uploads`, { name, size, mtime }),
  dropComplete: (token: string, id: string) => api<{ name: string; size: number }>('POST', `/api/public/${token}/uploads/${id}/complete`),
  dropAbort: (token: string, id: string) => api<{ ok: true }>('DELETE', `/api/public/${token}/uploads/${id}`),

  publicZipUrl: (token: string, paths: string[], name?: string, part?: ZipPart | null) => `/api/public/${token}/zip?` + zipQuery(paths, name, part),
  publicZipPlan: (token: string, paths: string[]) => api<ZipPlan>('GET', `/api/public/${token}/zip/plan?` + zipQuery(paths)),

  // admin
  adminUsers: () => api<{ users: AdminUser[] }>('GET', '/api/admin/users'),
  adminCreateUser: (u: { username: string; password: string; role: string; scope: string; mustChangePassword: boolean; quota: number }) =>
    api<{ user: AdminUser }>('POST', '/api/admin/users', u),
  adminUpdateUser: (id: number, patch: Partial<{ role: string; scope: string; disabled: boolean; password: string; mustChangePassword: boolean; quota: number }>) =>
    api<{ user: AdminUser }>('PATCH', '/api/admin/users/' + id, patch),
  adminDeleteUser: (id: number) => api<{ ok: true }>('DELETE', '/api/admin/users/' + id),
  adminDirs: (path: string) => api<{ path: string; dirs: string[] }>('GET', '/api/admin/dirs' + q({ path })),
  adminAudit: (before?: number, limit = 100) => api<{ entries: AuditEntry[] }>('GET', '/api/admin/audit' + q({ before, limit })),
  adminDashboard: () => api<Dashboard>('GET', '/api/admin/dashboard'),
  adminRevokeSession: (id: string) => api<{ ok: true }>('DELETE', '/api/admin/sessions/' + encodeURIComponent(id)),
}
