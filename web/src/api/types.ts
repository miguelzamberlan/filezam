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
  // presentes só na listagem paginada (?limit=): total após o filtro de ocultos, offset desta página, ocultos excluídos
  total?: number
  offset?: number
  hidden?: number
}

export interface ListPage {
  offset: number
  limit: number
  sort: 'name' | 'size' | 'mtime' | 'type'
  dir: 'asc' | 'desc'
  hidden: boolean
}

export interface User {
  id: number
  username: string
  role: 'admin' | 'user'
  restricted: boolean
  mustChangePassword: boolean
  totpEnabled: boolean
  totpRequired: boolean // admin obrigado a cadastrar o 2FA antes de usar o app
}

export interface AdminUser {
  id: number
  username: string
  role: 'admin' | 'user'
  scope: string
  mustChangePassword: boolean
  disabled: boolean
  quota: number // bytes; 0 = sem limite
  totpEnabled: boolean
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
  publicUrl: string
  trashRetention: number // segundos; 0 = lixeira desativada
  require2fa: boolean
  previewMaxText: number
  version: string
  // Ligados pelo administrador em Configurações; desligados, a interface nem oferece a opção.
  slugsEnabled: boolean
  dropEnabled: boolean
  dropMaxTtl: number // segundos; nunca passa de 30 dias
  dropMaxQuota: number
  dropFileMax: number
  dropMaxFiles: number
  extractEnabled: boolean
}

export interface Settings {
  slugsEnabled: boolean
  dropEnabled: boolean
  dropMaxQuota: number
  dropMaxTtl: number
  dropFileMax: number
  dropMaxFiles: number
  dropMaxLinks: number
  dropStaleAge: number
}

export type JobType = 'copy' | 'move' | 'delete' | 'extract' | 'archive'

export interface Job {
  id: string
  type: JobType
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
  label?: string
}

export interface JobRecord {
  id: string
  type: JobType
  label: string
  state: 'running' | 'done' | 'failed' | 'cancelled'
  done: number
  total: number
  bytesDone: number
  bytesTotal: number
  error?: string
  warnings: number
  startedAt: number
  finishedAt: number | null
}

export interface Favorite {
  id: number
  path: string
  name: string
  createdAt: number
}

export interface Share {
  id: number
  token: string
  slug: string
  mode: 'read' | 'drop'
  kind: 'dir' | 'file'
  hasPassword: boolean
  revoked: boolean
  quotaBytes: number
  usedBytes: number
  fileCount: number
  maxFileBytes: number
  maxFiles: number
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
  quota?: number // bytes, só quando o usuário tem cota
  quotaUsed?: number
}

export interface SearchHit {
  dir: string // pasta que contém o item, relativa ao escopo ("" = raiz)
  entry: Entry
}

export interface SearchResult {
  path: string
  q: string
  results: SearchHit[]
  partial: boolean
  source?: 'index' | 'walk'
  indexedAt?: number | null // unix s
}

// Travado, a resposta traz só kind/mode/expiresAt/now: nome e limites só depois do unlock,
// senão um apelido adivinhado já contaria o que existe do outro lado.
export interface PublicInfo {
  name?: string
  kind: 'dir' | 'file'
  mode: 'read' | 'drop'
  expiresAt: number
  now: number
  locked: boolean
  size?: number
  mtime?: number
  fileName?: string
  // Só no modo drop, destravado.
  quotaBytes?: number
  usedBytes?: number
  fileCount?: number
  maxFileBytes?: number
  maxFiles?: number
  chunkSize?: number
  maxParallel?: number
  mine?: DropUpload[]
}

export interface DropUpload {
  name: string
  size: number
  at: number
}

export interface TrashItem {
  id: string
  name: string
  path: string // caminho original, relativo ao escopo
  type: EntryType
  size: number
  deletedAt: number
  by: string
}

export interface IndexStatus {
  enabled: boolean
  ready?: boolean
  running?: boolean
  entries?: number
  lastFullAt?: number | null
  interval?: number
}

export interface EntryInfo {
  path: string
  entry: Entry
  totals?: { files: number; dirs: number; bytes: number; partial: boolean }
  shares?: Share[]
  favorite?: boolean
}
