import { Api, ApiError, isRetryable } from '../api/client'
import { backoffMs, chunkRange } from './scheduler'
import { xhrSend } from './xhr'

/**
 * Fila de envio de um link público de recebimento.
 *
 * É deliberadamente separada do UploadManager autenticado: aqui não existe pasta, lote
 * multipart, sobrescrita, diálogo de conflito nem retomada de sessão pendente — o visitante
 * anônimo só empurra arquivos soltos. Reaproveitar aquele gerenciador significaria carregar
 * toda a máquina de estado dele (e o singleton global) numa página que vive fora da sessão.
 */

export type DropState = 'queued' | 'uploading' | 'done' | 'failed'

export interface DropItem {
  id: number
  name: string
  size: number
  sent: number
  state: DropState
  errorCode?: string
}

export interface DropSnapshot {
  version: number
  items: DropItem[]
  active: number
  bytesTotal: number
  bytesDone: number
  failed: number
}

export interface DropLimits {
  chunkSize: number
  maxParallel: number
  maxFileBytes: number
}

const MAX_ATTEMPTS = 6
/** Erros do próprio link: insistir não adianta e o visitante precisa ver o motivo. */
const FATAL = new Set(['drop_full', 'drop_file_limit', 'drop_count_exceeded', 'invalid_name', 'share_locked', 'not_found'])

/** Um envio só vira sessão em blocos acima do bloco; abaixo disso vai num PUT só. */
export function dropMode(size: number, chunkSize: number): 'single' | 'chunked' {
  return size <= chunkSize ? 'single' : 'chunked'
}

/** isFatal: erro que o link impôs (cota, teto, senha) nunca deve entrar em retry. */
export function isFatal(code: string): boolean {
  return FATAL.has(code)
}

export class DropUploader {
  private token = ''
  private limits: DropLimits = { chunkSize: 16 << 20, maxParallel: 2, maxFileBytes: 0 }
  private items: DropItem[] = []
  private files = new Map<number, File>()
  private attempts = new Map<number, number>()
  private aborts = new Map<number, () => void>()
  private nextId = 1
  private active = 0
  private pumping = false
  private listeners = new Set<() => void>()
  private snap: DropSnapshot = { version: 0, items: [], active: 0, bytesTotal: 0, bytesDone: 0, failed: 0 }
  private dirty = true

  /** Chamado a cada arquivo concluído, para a página recarregar cota e lista de envios. */
  onDone: () => void = () => {}

  configure(token: string, limits: Partial<DropLimits>) {
    this.token = token
    this.limits = { ...this.limits, ...limits }
    this.pump()
  }

  add(files: File[]) {
    for (const f of files) {
      this.items.push({ id: this.nextId, name: f.name, size: f.size, sent: 0, state: 'queued' })
      this.files.set(this.nextId, f)
      this.nextId++
    }
    this.markDirty()
    this.pump()
  }

  retry(id: number) {
    const it = this.items.find((i) => i.id === id)
    if (!it || it.state !== 'failed') return
    it.state = 'queued'
    it.sent = 0
    it.errorCode = undefined
    this.attempts.set(id, 0)
    this.markDirty()
    this.pump()
  }

  cancel(id: number) {
    this.aborts.get(id)?.()
    const it = this.items.find((i) => i.id === id)
    if (it && it.state !== 'done') {
      this.items = this.items.filter((i) => i.id !== id)
      this.files.delete(id)
      this.markDirty()
    }
  }

  clearDone() {
    this.items = this.items.filter((i) => i.state !== 'done')
    this.markDirty()
  }

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => void this.listeners.delete(fn)
  }

  getSnapshot = (): DropSnapshot => {
    if (this.dirty) {
      this.dirty = false
      this.snap = {
        version: this.snap.version + 1,
        items: [...this.items],
        active: this.active,
        bytesTotal: this.items.reduce((n, i) => n + i.size, 0),
        bytesDone: this.items.reduce((n, i) => n + (i.state === 'done' ? i.size : i.sent), 0),
        failed: this.items.filter((i) => i.state === 'failed').length,
      }
    }
    return this.snap
  }

  private markDirty() {
    this.dirty = true
    for (const fn of this.listeners) fn()
  }

  private pump() {
    if (this.pumping || !this.token) return
    this.pumping = true
    try {
      while (this.active < this.limits.maxParallel) {
        const it = this.items.find((i) => i.state === 'queued')
        if (!it) return
        it.state = 'uploading'
        this.active++
        this.markDirty()
        void this.send(it)
      }
    } finally {
      this.pumping = false
    }
  }

  private async send(it: DropItem) {
    const file = this.files.get(it.id)
    try {
      if (!file) throw new ApiError(0, 'internal', 'file is gone')
      // O servidor recusa acima do teto por arquivo de qualquer jeito; falhar aqui poupa a
      // subida inteira de um arquivo que nunca seria aceito.
      if (this.limits.maxFileBytes > 0 && it.size > this.limits.maxFileBytes) {
        throw new ApiError(413, 'drop_file_limit', 'file above the per-file limit')
      }
      await (dropMode(it.size, this.limits.chunkSize) === 'single' ? this.sendSingle(it, file) : this.sendChunked(it, file))
      it.state = 'done'
      it.sent = it.size
      this.files.delete(it.id)
      this.onDone()
    } catch (e) {
      this.fail(it, e)
    } finally {
      this.aborts.delete(it.id)
      this.active--
      this.markDirty()
      this.pump()
    }
  }

  private async sendSingle(it: DropItem, file: File): Promise<void> {
    const url = `/api/public/${this.token}/content?name=${encodeURIComponent(file.name)}&mtime=${file.lastModified}`
    const h = xhrSend('PUT', url, file, (sent) => this.progress(it, sent), { 'Content-Type': 'application/octet-stream' })
    this.aborts.set(it.id, h.abort)
    await h.promise
  }

  private async sendChunked(it: DropItem, file: File): Promise<void> {
    const s = await Api.dropCreate(this.token, file.name, file.size, file.lastModified)
    const received = new Set(s.received)
    try {
      for (let i = 0; i < s.chunks; i++) {
        if (received.has(i)) continue
        const [start, end] = chunkRange(i, s.chunkSize, s.size)
        const base = i * s.chunkSize
        const h = xhrSend('PUT', `/api/public/${this.token}/uploads/${s.id}?index=${i}`, file.slice(start, end), (sent) => this.progress(it, base + sent), {
          'Content-Type': 'application/octet-stream',
        })
        this.aborts.set(it.id, h.abort)
        await h.promise
      }
      await Api.dropComplete(this.token, s.id)
    } catch (e) {
      // Sessão morta ou envio cancelado: não deixa bytes reservados segurando a cota do link.
      await Api.dropAbort(this.token, s.id).catch(() => {})
      throw e
    }
  }

  private progress(it: DropItem, sent: number) {
    it.sent = Math.min(sent, it.size)
    this.markDirty()
  }

  private fail(it: DropItem, e: unknown) {
    const err = e instanceof ApiError ? e : new ApiError(0, 'internal', String(e))
    if (err.code === 'aborted') {
      this.items = this.items.filter((i) => i.id !== it.id)
      return
    }
    const n = (this.attempts.get(it.id) ?? 0) + 1
    this.attempts.set(it.id, n)
    if (!isFatal(err.code) && isRetryable(err) && n < MAX_ATTEMPTS) {
      it.state = 'queued'
      it.sent = 0
      setTimeout(() => this.pump(), backoffMs(n))
      return
    }
    it.state = 'failed'
    it.errorCode = err.code
  }
}
