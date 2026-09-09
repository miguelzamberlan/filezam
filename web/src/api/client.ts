import type {
  AdminUser, AppConfig, AuditEntry, Conflict, Entry, Favorite, Job, Listing, Share, UploadSession, User,
} from './types'

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string, public extra: Record<string, unknown> = {}) {
    super(message)
  }
}

export const isRetryable = (e: unknown) =>
  e instanceof ApiError && (e.status === 429 || e.status === 502 || e.status === 503 || e.status === 504 || e.status === 0)

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

export const Api = {
  // auth
  login: (username: string, password: string) => api<{ user: User }>('POST', '/api/auth/login', { username, password }),
  logout: () => api<{ ok: true }>('POST', '/api/auth/logout'),
  me: () => api<{ user: User }>('GET', '/api/auth/me'),
  changePassword: (current: string, next: string) => api<{ user: User }>('POST', '/api/auth/password', { current, new: next }),
  config: () => api<AppConfig>('GET', '/api/config'),

  // files
  list: (path: string) => api<Listing>('GET', '/api/files' + q({ path })),
  stat: (path: string) => api<{ path: string; entry: Entry }>('GET', '/api/files/stat' + q({ path })),
  contentUrl: (path: string, inline = false) => '/api/files/content' + q({ path, inline: inline ? 1 : undefined }),
  zipUrl: (paths: string[], name?: string) => {
    const sp = new URLSearchParams()
    for (const p of paths) sp.append('path', p)
    if (name) sp.set('name', name)
    return '/api/files/zip?' + sp.toString()
  },
  mkdir: (path: string) => api<{ path: string; entry: Entry }>('POST', '/api/files/mkdir', { path }),
  rename: (path: string, newName: string) => api<{ path: string; entry: Entry }>('POST', '/api/files/rename', { path, newName }),
  delete: (paths: string[]) => api<{ job: Job }>('POST', '/api/files/delete', { paths }),
  copy: (sources: string[], destDir: string, onConflict: Conflict) =>
    api<{ job: Job }>('POST', '/api/files/copy', { sources, destDir, onConflict }),
  move: (sources: string[], destDir: string, onConflict: Conflict) =>
    api<{ job: Job }>('POST', '/api/files/move', { sources, destDir, onConflict }),

  // jobs
  jobs: () => api<{ jobs: Job[] }>('GET', '/api/jobs'),
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
  createShare: (path: string, expiresIn: number, name?: string) =>
    api<{ share: Share; token: string; url: string }>('POST', '/api/shares', { path, expiresIn, name: name ?? '' }),
  deleteShare: (id: number) => api<{ ok: true }>('DELETE', '/api/shares/' + id),

  // public
  publicInfo: (token: string) => api<{ name: string; expiresAt: number; now: number }>('GET', '/api/public/' + token),
  publicList: (token: string, path: string) => api<Listing>('GET', `/api/public/${token}/list` + q({ path })),
  publicContentUrl: (token: string, path: string, inline = false) =>
    `/api/public/${token}/content` + q({ path, inline: inline ? 1 : undefined }),
  publicZipUrl: (token: string, paths: string[]) => {
    const sp = new URLSearchParams()
    for (const p of paths) sp.append('path', p)
    return `/api/public/${token}/zip?` + sp.toString()
  },

  // admin
  adminUsers: () => api<{ users: AdminUser[] }>('GET', '/api/admin/users'),
  adminCreateUser: (u: { username: string; password: string; role: string; scope: string; mustChangePassword: boolean }) =>
    api<{ user: AdminUser }>('POST', '/api/admin/users', u),
  adminUpdateUser: (id: number, patch: Partial<{ role: string; scope: string; disabled: boolean; password: string; mustChangePassword: boolean }>) =>
    api<{ user: AdminUser }>('PATCH', '/api/admin/users/' + id, patch),
  adminDeleteUser: (id: number) => api<{ ok: true }>('DELETE', '/api/admin/users/' + id),
  adminDirs: (path: string) => api<{ path: string; dirs: string[] }>('GET', '/api/admin/dirs' + q({ path })),
  adminAudit: (before?: number, limit = 100) => api<{ entries: AuditEntry[] }>('GET', '/api/admin/audit' + q({ before, limit })),
}
