export type EntryType = 'file' | 'dir' | 'other'

export interface Entry {
  name: string
  type: EntryType
  size: number
  mtime: number
  link?: boolean
  nameInvalid?: boolean
}

export interface Listing {
  path: string
  entries: Entry[]
}

export interface User {
  id: number
  username: string
  role: 'admin' | 'user'
  restricted: boolean
  mustChangePassword: boolean
}

export interface AdminUser {
  id: number
  username: string
  role: 'admin' | 'user'
  scope: string
  mustChangePassword: boolean
  disabled: boolean
  lockedUntil: number | null
  createdAt: number
  updatedAt: number
}

export interface AppConfig {
  chunkSize: number
  batchMaxFiles: number
  batchMaxBytes: number
  batchFileMax: number
  maxParallel: number
  shareMaxTtl: number
  previewMaxText: number
  version: string
}

export interface Job {
  id: string
  type: 'copy' | 'move' | 'delete'
  state: 'running' | 'done' | 'failed' | 'cancelled'
  done: number
  total: number
  bytesDone: number
  bytesTotal: number
  current?: string
  error?: string
  warnings?: string[]
  startedAt: number
  finishedAt?: number
  dirs?: string[]
}

export interface Favorite {
  id: number
  path: string
  name: string
  createdAt: number
}

export interface Share {
  id: number
  path: string
  name: string
  createdBy: string
  mine: boolean
  createdAt: number
  expiresAt: number
  expired: boolean
  accessCount: number
  lastAccessAt: number | null
}

export interface UploadSession {
  id: string
  dir: string
  name: string
  size: number
  mtime: number | null
  chunkSize: number
  chunks: number
  received: number[]
  overwrite: boolean
  createdAt: number
  updatedAt: number
}

export interface AuditEntry {
  id: number
  ts: number
  userId: number | null
  username: string
  ip: string
  action: string
  detail: string
}

export type Conflict = 'rename' | 'overwrite' | 'skip'

export interface DiskUsage {
  total: number
  free: number
  used: number
}

export interface EntryInfo {
  path: string
  entry: Entry
  totals?: { files: number; dirs: number; bytes: number; partial: boolean }
  shares?: Share[]
  favorite?: boolean
}
