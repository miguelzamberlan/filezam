import { Api, ApiError, isRetryable } from '../api/client'
import type { AppConfig, Conflict, UploadSession } from '../api/types'
import { basename, dirname, join, uniqueName } from '../lib/paths'
import { backoffMs, chunkRange, classify, pickBatch, type Mode } from './scheduler'
import { xhrSend, type XhrHandle } from './xhr'
import type { PickedFile } from './walk'

export type ItemState = 'queued' | 'uploading' | 'done' | 'failed' | 'cancelled' | 'skipped' | 'conflict'

export interface UploadItem {
  id: number
  file: File
  destDir: string // scope-relative folder the user dropped into
  relPath: string // path of the file relative to destDir (may contain folders)
  size: number
  mode: Mode
  state: ItemState
  sent: number
  attempts: number
  overwrite: boolean
  error?: string
  errorCode?: string
  // chunked
  session?: { id: string; chunkSize: number; chunks: number; received: Set<number> }
  inflight: number
  handles: Set<XhrHandle>
}

export interface Snapshot {
  version: number
  items: UploadItem[]
  active: number
  paused: boolean
  bytesTotal: number
  bytesDone: number
  filesTotal: number
  filesDone: number
  failed: number
  speed: number // bytes/sec (EMA)
  pending: UploadSession[]
}

export interface ConflictAnswer {
  choice: Conflict | 'cancel'
  all: boolean
}

type ConflictHandler = (item: UploadItem) => Promise<ConflictAnswer>

const CHUNK_SLOTS_PER_FILE = 2
const MAX_ATTEMPTS = 6

export class UploadManager {
  private items: UploadItem[] = []
  private nextId = 1
  private listeners = new Set<() => void>()
  private snap: Snapshot = {
    version: 0, items: [], active: 0, paused: false, bytesTotal: 0, bytesDone: 0, filesTotal: 0, filesDone: 0, failed: 0, speed: 0, pending: [],
  }
  private dirty = true
  private snapTimer: number | null = null
  private active = 0
  private paused = false
  private pumping = false
  private cfg: AppConfig | null = null
  private conflictDefault: Conflict | null = null
  private dirTimers = new Map<string, number>()
  private lastTick = performance.now()
  private lastSent = 0
  private speed = 0
  private pending: UploadSession[] = []

  onConflict: ConflictHandler = async () => ({ choice: 'skip', all: false })
  onDirChanged: (dir: string) => void = () => {}
  onError: (msg: string) => void = () => {}

  configure(cfg: AppConfig) {
    this.cfg = cfg
    this.pump()
  }

  async loadPending() {
    try {
      const { uploads } = await Api.uploadList()
      this.pending = uploads
      this.markDirty()
    } catch {
      /* ignore */
    }
  }

  async discardPending() {
    for (const p of this.pending) {
      try {
        await Api.uploadAbort(p.id)
      } catch {
        /* ignore */
      }
    }
    this.pending = []
    this.markDirty()
  }

  // ---- public queue API ----

  add(files: PickedFile[], destDir: string) {
    if (!this.cfg) return
    const limits = { chunkSize: this.cfg.chunkSize, batchFileMax: this.cfg.batchFileMax, batchMaxFiles: this.cfg.batchMaxFiles, batchMaxBytes: this.cfg.batchMaxBytes }
    const seen = new Set(this.items.filter((i) => i.state === 'queued' || i.state === 'uploading').map((i) => join(i.destDir, i.relPath)))
    for (const f of files) {
      const key = join(destDir, f.relPath)
      if (seen.has(key)) continue
      seen.add(key)
      const item: UploadItem = {
        id: this.nextId++, file: f.file, destDir, relPath: f.relPath, size: f.file.size, mode: classify(f.file.size, limits),
        state: 'queued', sent: 0, attempts: 0, overwrite: false, inflight: 0, handles: new Set(),
      }
      this.matchPending(item)
      this.items.push(item)
    }
    this.markDirty()
    this.pump()
  }

  /** Attach a pending server session if this file matches it (resume after reload). */
  private matchPending(item: UploadItem) {
    if (item.mode !== 'chunked') return
    const dir = join(item.destDir, dirname(item.relPath))
    const name = basename(item.relPath)
    const idx = this.pending.findIndex((p) => p.dir === dir && p.name === name && p.size === item.size && (p.mtime == null || Math.abs(p.mtime - item.file.lastModified) < 1000))
    if (idx < 0) return
    const p = this.pending[idx]
    this.pending.splice(idx, 1)
    item.session = { id: p.id, chunkSize: p.chunkSize, chunks: p.chunks, received: new Set(p.received) }
    item.overwrite = p.overwrite
    item.sent = p.received.reduce((acc, i) => acc + (chunkRange(i, p.chunkSize, p.size)[1] - chunkRange(i, p.chunkSize, p.size)[0]), 0)
  }

  pause() {
    this.paused = true
    this.markDirty()
  }

  resume() {
    this.paused = false
    this.markDirty()
    this.pump()
  }

  cancel(id: number) {
    const it = this.items.find((i) => i.id === id)
    if (!it) return
    this.abortItem(it, 'cancelled')
  }

  cancelAll() {
    for (const it of this.items) if (it.state === 'queued' || it.state === 'uploading' || it.state === 'conflict') this.abortItem(it, 'cancelled')
    this.conflictDefault = null
  }

  retryFailed() {
    for (const it of this.items) {
      if (it.state === 'failed' || it.state === 'cancelled') {
        it.state = 'queued'
        it.attempts = 0
        it.error = undefined
        it.errorCode = undefined
        if (it.session) it.sent = 0, (it.session = undefined)
      }
    }
    this.markDirty()
    this.pump()
  }

  clearDone() {
    this.items = this.items.filter((i) => i.state !== 'done' && i.state !== 'skipped' && i.state !== 'cancelled')
    this.markDirty()
  }

  resetConflictDefault() {
    this.conflictDefault = null
  }

  // ---- subscription ----

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  getSnapshot = () => this.snap

  private markDirty() {
    this.dirty = true
    if (this.snapTimer == null) this.snapTimer = window.setTimeout(() => this.flushSnapshot(), 250)
  }

  private flushSnapshot() {
    this.snapTimer = null
    if (!this.dirty) return
    this.dirty = false
    let bytesTotal = 0, bytesDone = 0, filesDone = 0, failed = 0, filesTotal = 0
    for (const it of this.items) {
      if (it.state === 'cancelled' || it.state === 'skipped') continue
      filesTotal++
      bytesTotal += it.size
      if (it.state === 'done') {
        filesDone++
        bytesDone += it.size
      } else if (it.state === 'failed') failed++
      else bytesDone += Math.min(it.sent, it.size)
    }
    const now = performance.now()
    const dt = (now - this.lastTick) / 1000
    if (dt >= 0.5) {
      const inst = Math.max(0, (bytesDone - this.lastSent) / dt)
      this.speed = this.speed === 0 ? inst : this.speed * 0.6 + inst * 0.4
      this.lastTick = now
      this.lastSent = bytesDone
      if (this.active === 0) this.speed = 0
    }
    this.snap = {
      version: this.snap.version + 1, items: [...this.items], active: this.active, paused: this.paused,
      bytesTotal, bytesDone, filesTotal, filesDone, failed, speed: this.speed, pending: this.pending,
    }
    for (const fn of this.listeners) fn()
    if (this.active > 0) this.markDirty() // keep the speed sampler ticking while busy
  }

  // ---- scheduling ----

  private get slots() {
    return this.cfg?.maxParallel ?? 4
  }

  private pump() {
    if (this.pumping || !this.cfg) return
    this.pumping = true
    try {
      while (!this.paused && this.active < this.slots) {
        const work = this.nextWork()
        if (!work) break
        this.active++
        work().finally(() => {
          this.active--
          this.markDirty()
          this.pump()
        })
      }
    } finally {
      this.pumping = false
    }
    this.markDirty()
  }

  private nextWork(): (() => Promise<void>) | null {
    const cfg = this.cfg!
    const limits = { chunkSize: cfg.chunkSize, batchFileMax: cfg.batchFileMax, batchMaxFiles: cfg.batchMaxFiles, batchMaxBytes: cfg.batchMaxBytes }
    // 1. chunked items already in progress get priority for their next chunk
    for (const it of this.items) {
      if (it.state === 'uploading' && it.mode === 'chunked' && it.session && it.inflight < CHUNK_SLOTS_PER_FILE) {
        const idx = this.nextChunkIndex(it)
        if (idx != null) return () => this.sendChunk(it, idx)
      }
    }
    // 2. queued items in order
    const queued = this.items.filter((i) => i.state === 'queued')
    const first = queued[0]
    if (!first) return null
    if (first.mode === 'batch') {
      const group = pickBatch(queued.filter((i) => i.mode === 'batch'), limits)
      return () => this.sendBatch(group)
    }
    if (first.mode === 'single') return () => this.sendSingle(first)
    return () => this.startChunked(first)
  }

  private nextChunkIndex(it: UploadItem): number | null {
    const s = it.session!
    for (let i = 0; i < s.chunks; i++) if (!s.received.has(i) && !this.inflightChunks.get(it.id)?.has(i)) return i
    return null
  }

  private inflightChunks = new Map<number, Set<number>>()

  // ---- transfers ----

  private async sendBatch(group: UploadItem[]) {
    for (const it of group) {
      it.state = 'uploading'
      it.attempts++
    }
    const fd = new FormData()
    const meta = { files: group.map((it) => ({ path: it.relPath, mtime: it.file.lastModified, size: it.size })) }
    fd.append('meta', JSON.stringify(meta))
    group.forEach((it, i) => fd.append(String(i), it.file, 'f'))
    const url = `/api/files/batch?dir=${encodeURIComponent(group[0].destDir)}&overwrite=${group[0].overwrite ? 1 : 0}`
    const total = group.reduce((a, b) => a + b.size, 0)
    const handle = xhrSend('POST', url, fd, (loaded) => {
      // distribute progress proportionally
      let remaining = Math.min(loaded, total)
      for (const it of group) {
        const part = Math.min(it.size, remaining)
        it.sent = part
        remaining -= part
      }
      this.markDirty()
    })
    for (const it of group) it.handles.add(handle)
    try {
      const res = (await handle.promise) as { results: { ok: boolean; code?: string; error?: string }[] }
      group.forEach((it, i) => {
        const r = res.results[i]
        if (r?.ok) this.finish(it)
        else this.handleFailure(it, new ApiError(r?.code === 'exists' ? 409 : 400, r?.code ?? 'internal', r?.error ?? 'error'))
      })
    } catch (e) {
      for (const it of group) this.handleFailure(it, e)
    } finally {
      for (const it of group) it.handles.delete(handle)
    }
  }

  private async sendSingle(it: UploadItem) {
    it.state = 'uploading'
    it.attempts++
    const path = join(it.destDir, it.relPath)
    const url = `/api/files/content?path=${encodeURIComponent(path)}&mtime=${it.file.lastModified}&overwrite=${it.overwrite ? 1 : 0}`
    const handle = xhrSend('PUT', url, it.file, (loaded) => {
      it.sent = loaded
      this.markDirty()
    }, { 'Content-Type': 'application/octet-stream' })
    it.handles.add(handle)
    try {
      await handle.promise
      this.finish(it)
    } catch (e) {
      this.handleFailure(it, e)
    } finally {
      it.handles.delete(handle)
    }
  }

  private async startChunked(it: UploadItem) {
    it.state = 'uploading'
    it.attempts++
    if (!it.session) {
      const dir = join(it.destDir, dirname(it.relPath))
      const name = basename(it.relPath)
      try {
        const s = await Api.uploadCreate(dir, name, it.size, it.file.lastModified, it.overwrite)
        it.session = { id: s.id, chunkSize: s.chunkSize, chunks: s.chunks, received: new Set(s.received) }
      } catch (e) {
        if (e instanceof ApiError && e.code === 'upload_in_progress') {
          await this.loadPending()
          this.matchPending(it)
          if (!it.session) {
            it.state = 'failed'
            it.errorCode = e.code
            it.error = e.message
            this.markDirty()
            return
          }
        } else {
          this.handleFailure(it, e)
          return
        }
      }
    }
    if (it.session.chunks === 0) {
      await this.completeChunked(it)
      return
    }
    // the pump will now schedule chunks for this item
    this.markDirty()
  }

  private async sendChunk(it: UploadItem, index: number) {
    const s = it.session!
    let set = this.inflightChunks.get(it.id)
    if (!set) this.inflightChunks.set(it.id, (set = new Set()))
    set.add(index)
    it.inflight++
    const [start, end] = chunkRange(index, s.chunkSize, it.size)
    const blob = it.file.slice(start, end)
    let last = 0
    const handle = xhrSend('PUT', `/api/uploads/${s.id}?index=${index}`, blob, (loaded) => {
      it.sent += loaded - last
      last = loaded
      this.markDirty()
    }, { 'Content-Type': 'application/octet-stream' })
    it.handles.add(handle)
    try {
      await handle.promise
      s.received.add(index)
      it.sent += end - start - last
      if (s.received.size === s.chunks && it.inflight === 1) await this.completeChunked(it)
    } catch (e) {
      it.sent -= last
      if (e instanceof ApiError && e.code === 'not_found') {
        // session vanished (cleanup or abort elsewhere): restart from scratch
        it.session = undefined
        it.sent = 0
        it.state = 'queued'
        this.markDirty()
      } else if (it.state === 'uploading') {
        this.handleFailure(it, e)
      }
    } finally {
      it.handles.delete(handle)
      set.delete(index)
      it.inflight--
    }
  }

  private async completeChunked(it: UploadItem) {
    const s = it.session!
    try {
      await Api.uploadComplete(s.id)
      this.finish(it)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'incomplete') {
        const missing = (e.extra.missing as number[]) ?? []
        for (const m of missing) s.received.delete(m)
        this.markDirty()
        return
      }
      if (e instanceof ApiError && e.code === 'exists') {
        // target appeared meanwhile: ask, then either overwrite (recreate session) or skip
        await this.handleConflict(it, true)
        return
      }
      this.handleFailure(it, e)
    }
  }

  private finish(it: UploadItem) {
    it.state = 'done'
    it.sent = it.size
    it.session = undefined
    this.notifyDir(join(it.destDir, dirname(it.relPath)))
    this.markDirty()
  }

  private handleFailure(it: UploadItem, e: unknown) {
    if (it.state === 'cancelled') return
    if (e instanceof ApiError && e.code === 'exists') {
      void this.handleConflict(it, false)
      return
    }
    if (e instanceof ApiError && e.code === 'aborted') return
    if (e instanceof ApiError && e.code === 'no_space') {
      it.state = 'failed'
      it.errorCode = e.code
      it.error = e.message
      this.pause()
      this.onError('no_space')
      this.markDirty()
      return
    }
    if (isRetryable(e) && it.attempts < MAX_ATTEMPTS) {
      it.state = 'queued'
      const delay = backoffMs(it.attempts)
      window.setTimeout(() => this.pump(), delay)
      this.markDirty()
      return
    }
    it.state = 'failed'
    it.errorCode = e instanceof ApiError ? e.code : 'network'
    it.error = e instanceof Error ? e.message : String(e)
    this.markDirty()
  }

  private conflictQueue: Promise<void> = Promise.resolve()

  private handleConflict(it: UploadItem, afterChunks: boolean): Promise<void> {
    it.state = 'conflict'
    this.markDirty()
    // serialize dialogs so "apply to all" takes effect for the rest
    this.conflictQueue = this.conflictQueue.then(async () => {
      if (it.state !== 'conflict') return
      let choice: Conflict | 'cancel'
      if (this.conflictDefault) choice = this.conflictDefault
      else {
        const ans = await this.onConflict(it)
        choice = ans.choice
        if (ans.all && choice !== 'cancel') this.conflictDefault = choice
      }
      if (it.state !== 'conflict') return
      switch (choice) {
        case 'overwrite':
          it.overwrite = true
          break
        case 'rename': {
          const name = basename(it.relPath)
          const taken = new Set([name, ...this.items.filter((o) => o !== it && o.destDir === it.destDir && dirname(o.relPath) === dirname(it.relPath)).map((o) => basename(o.relPath))])
          it.relPath = join(dirname(it.relPath), uniqueName(name, taken))
          break
        }
        case 'skip':
          it.state = 'skipped'
          if (afterChunks && it.session) void Api.uploadAbort(it.session.id).catch(() => {})
          it.session = undefined
          this.markDirty()
          return
        case 'cancel':
          this.cancelAll()
          return
      }
      if (afterChunks && it.session) {
        // chunks are on the server; a new session is required with the new name/overwrite flag
        void Api.uploadAbort(it.session.id).catch(() => {})
        it.session = undefined
      }
      it.sent = 0
      it.attempts = 0
      it.state = 'queued'
      this.markDirty()
      this.pump()
    })
    return this.conflictQueue
  }

  private abortItem(it: UploadItem, state: ItemState) {
    const prev = it.state
    it.state = state
    for (const h of it.handles) h.abort()
    it.handles.clear()
    if (it.session && prev !== 'done') {
      const id = it.session.id
      it.session = undefined
      void Api.uploadAbort(id).catch(() => {})
    }
    this.markDirty()
  }

  private notifyDir(dir: string) {
    if (this.dirTimers.has(dir)) return
    this.dirTimers.set(dir, window.setTimeout(() => {
      this.dirTimers.delete(dir)
      this.onDirChanged(dir)
    }, this.active > 1 ? 2000 : 300))
  }
}

export const uploadManager = new UploadManager()
