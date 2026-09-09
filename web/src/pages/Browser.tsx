import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { Conflict, Entry } from '../api/types'
import { useAuth, useFavorites, useInvalidateDirs, useListing } from '../hooks'
import { basename, decodePath, dirname, encodePath, join } from '../lib/paths'
import { sortEntries } from '../lib/naturalSort'
import { useUI } from '../store/ui'
import { useJobs } from '../store/jobs'
import { S, errorMessage } from '../strings'
import { uploadManager } from '../upload/manager'
import { collectFromDataTransfer, collectFromFileList, type PickedFile } from '../upload/walk'
import FileList from '../components/FileList'
import Breadcrumb from '../components/Breadcrumb'
import ContextMenu, { type MenuItem } from '../components/ContextMenu'
import Preview, { previewKind } from '../components/Preview'
import ShareDialog from '../components/ShareDialog'
import { dialogs, toast } from '../components/dialogs'
import { useUploads } from '../components/UploadPanel'
import {
  IArrowUp, ICopy, IDownload, IEdit, IFolderPlus, IGrid, IList, IPaste, IRefresh, IScissors, IShare, IStar, ITrash, IUpload, ISpinner, IEye,
} from '../components/Icons'

function triggerDownload(url: string) {
  const a = document.createElement('a')
  a.href = url
  a.download = ''
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

export default function Browser() {
  const params = useParams()
  const path = decodePath(params['*'] ?? '')
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { user } = useAuth()
  const listing = useListing(path)
  const invalidate = useInvalidateDirs()
  const favs = useFavorites()
  const track = useJobs((s) => s.track)
  const uploads = useUploads()
  const cfg = useQuery({ queryKey: ['config'], queryFn: () => Api.config(), staleTime: Infinity }).data
  const ui = useUI()
  const [menu, setMenu] = useState<{ x: number; y: number; entry: Entry | null } | null>(null)
  const [preview, setPreview] = useState<number | null>(null)
  const [share, setShare] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [dragOverName, setDragOverName] = useState<string | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const dirInput = useRef<HTMLInputElement>(null)
  const typeahead = useRef({ buf: '', t: 0 })

  const entries = useMemo(() => {
    const all = listing.data?.entries ?? []
    const f = ui.filter.trim().toLowerCase()
    const filtered = f ? all.filter((e) => e.name.toLowerCase().includes(f)) : all
    return sortEntries(filtered, ui.sort)
  }, [listing.data, ui.filter, ui.sort])

  const byName = useMemo(() => new Map(entries.map((e) => [e.name, e])), [entries])
  const selectedEntries = useMemo(() => entries.filter((e) => ui.selection.has(e.name)), [entries, ui.selection])
  const cutNames = useMemo(() => (ui.clipboard?.op === 'cut' && ui.clipboard.dir === path ? new Set(ui.clipboard.names) : undefined), [ui.clipboard, path])
  const isFav = favs.data?.favorites.find((f) => f.path === path)

  useEffect(() => {
    ui.clearSelection()
    ui.setFocused(null)
    ui.setFilter('')
    setPreview(null)
    listRef.current?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path])

  const go = useCallback((p: string) => navigate('/b' + (p ? '/' + encodePath(p) : '')), [navigate])
  const refresh = () => qc.invalidateQueries({ queryKey: ['list', path] })
  const fail = (e: unknown) => toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')

  // ---- selection ----
  const select = (e: Entry, ev: MouseEvent | { shiftKey: boolean; ctrlKey: boolean; metaKey: boolean }) => {
    const sel = new Set(ui.selection)
    if (ev.shiftKey && ui.anchor && byName.has(ui.anchor)) {
      const a = entries.findIndex((x) => x.name === ui.anchor)
      const b = entries.findIndex((x) => x.name === e.name)
      const [lo, hi] = a < b ? [a, b] : [b, a]
      if (!ev.ctrlKey && !ev.metaKey) sel.clear()
      for (let i = lo; i <= hi; i++) sel.add(entries[i].name)
      ui.setSelection(sel, ui.anchor, e.name)
    } else if (ev.ctrlKey || ev.metaKey) {
      if (sel.has(e.name)) sel.delete(e.name)
      else sel.add(e.name)
      ui.setSelection(sel, e.name, e.name)
    } else {
      ui.setSelection(new Set([e.name]), e.name, e.name)
    }
  }

  const open = (e: Entry) => {
    if (e.nameInvalid) return
    if (e.type === 'dir') return go(join(path, e.name))
    if (e.type !== 'file') return
    const i = entries.indexOf(e)
    if (previewKind(e)) setPreview(i)
    else triggerDownload(Api.contentUrl(join(path, e.name)))
  }

  // ---- actions ----
  const newFolder = async () => {
    const name = await dialogs.prompt({ title: S.newFolder, label: S.newFolderName, initial: S.newFolder, validate: (v) => (v.includes('/') ? S.errorCodes.invalid_name : null) })
    if (!name) return
    try {
      await Api.mkdir(join(path, name))
      await qc.invalidateQueries({ queryKey: ['list', path] })
      ui.setSelection(new Set([name]), name, name)
    } catch (e) {
      fail(e)
    }
  }

  const rename = async (e?: Entry) => {
    const target = e ?? (selectedEntries.length === 1 ? selectedEntries[0] : undefined)
    if (!target) return
    const name = await dialogs.prompt({ title: S.rename, label: S.renameTo, initial: target.name, selectExt: target.type === 'file', validate: (v) => (v.includes('/') ? S.errorCodes.invalid_name : null) })
    if (!name || name === target.name) return
    try {
      await Api.rename(join(path, target.name), name)
      await qc.invalidateQueries({ queryKey: ['list', path] })
      qc.invalidateQueries({ queryKey: ['favorites'] })
      ui.setSelection(new Set([name]), name, name)
    } catch (e) {
      fail(e)
    }
  }

  const remove = async (list = selectedEntries) => {
    if (list.length === 0) return
    if (!(await dialogs.confirm({ title: S.deleteConfirm(list.length), message: list.length === 1 ? list[0].name : S.deleteWarning, danger: true, okLabel: S.delete }))) return
    try {
      const { job } = await Api.delete(list.map((e) => join(path, e.name)))
      track(job)
      ui.clearSelection()
      qc.invalidateQueries({ queryKey: ['favorites'] })
    } catch (e) {
      fail(e)
    }
  }

  const clip = (op: 'copy' | 'cut', list = selectedEntries) => {
    if (list.length === 0) return
    ui.setClipboard({ op, dir: path, names: list.map((e) => e.name) })
  }

  const paste = async (destDir = path) => {
    const c = ui.clipboard
    if (!c) return
    let policy: Conflict = 'rename'
    if (destDir === path) {
      const existing = new Set(entries.map((e) => e.name))
      if (c.names.some((n) => existing.has(n)) && !(c.op === 'cut' && c.dir === path)) {
        const ans = await dialogs.conflict(c.names.find((n) => existing.has(n))!)
        if (ans.choice === 'cancel') return
        policy = ans.choice
      }
    }
    try {
      const sources = c.names.map((n) => join(c.dir, n))
      const { job } = c.op === 'copy' ? await Api.copy(sources, destDir, policy) : await Api.move(sources, destDir, policy)
      track(job)
      if (c.op === 'cut') ui.setClipboard(null)
    } catch (e) {
      fail(e)
    }
  }

  const download = (list = selectedEntries) => {
    if (list.length === 1 && list[0].type === 'file') return triggerDownload(Api.contentUrl(join(path, list[0].name)))
    if (list.length === 0) return triggerDownload(Api.zipUrl([path], basename(path) || undefined))
    triggerDownload(Api.zipUrl(list.map((e) => join(path, e.name))))
  }

  const toggleFavorite = async (p = path) => {
    const f = favs.data?.favorites.find((x) => x.path === p)
    try {
      if (f) await Api.removeFavorite(f.id)
      else await Api.addFavorite(p)
      qc.invalidateQueries({ queryKey: ['favorites'] })
    } catch (e) {
      fail(e)
    }
  }

  const addFiles = async (files: PickedFile[], dest: string) => {
    if (files.length === 0) return
    uploadManager.resetConflictDefault()
    uploadManager.add(files, dest)
  }

  const onDrop = async (ev: DragEvent, dest = path) => {
    ev.preventDefault()
    setDragOver(false)
    setDragOverName(null)
    if (!ev.dataTransfer.types.includes('Files')) return
    const files: PickedFile[] = []
    const dirs = new Set<string>()
    let batch: PickedFile[] = []
    uploadManager.resetConflictDefault()
    await collectFromDataTransfer(
      ev.dataTransfer,
      (f) => {
        files.push(f)
        batch.push(f)
        if (batch.length >= 500) {
          uploadManager.add(batch, dest)
          batch = []
        }
      },
      (d) => dirs.add(d),
    )
    if (batch.length) uploadManager.add(batch, dest)
    // create folders that contain no files (uploads create the rest implicitly)
    const withFiles = new Set<string>()
    for (const f of files) {
      let d = dirname(f.relPath)
      while (d) {
        withFiles.add(d)
        d = dirname(d)
      }
    }
    for (const d of dirs) if (!withFiles.has(d)) await Api.mkdir(join(dest, d)).catch(() => {})
    if (files.length === 0 && dirs.size > 0) invalidate([dest])
  }

  // ---- keyboard ----
  const onKeyDown = (ev: React.KeyboardEvent) => {
    if (menu || preview !== null || share) return
    const tag = (ev.target as HTMLElement).tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
    const mod = ev.ctrlKey || ev.metaKey
    const idx = ui.focused ? entries.findIndex((e) => e.name === ui.focused) : -1
    const focusAt = (i: number, extend = false) => {
      i = Math.max(0, Math.min(entries.length - 1, i))
      const e = entries[i]
      if (!e) return
      if (extend) select(e, { shiftKey: true, ctrlKey: false, metaKey: false })
      else ui.setSelection(new Set([e.name]), e.name, e.name)
    }
    const pageSize = Math.max(1, Math.floor((listRef.current?.clientHeight ?? 400) / 34) - 1)
    switch (ev.key) {
      case 'ArrowDown':
        focusAt(idx + 1, ev.shiftKey)
        break
      case 'ArrowUp':
        if (ev.altKey) return void go(dirname(path))
        focusAt(idx - 1, ev.shiftKey)
        break
      case 'ArrowRight':
      case 'ArrowLeft':
        if (ui.view === 'grid') focusAt(idx + (ev.key === 'ArrowRight' ? 1 : -1), ev.shiftKey)
        else return
        break
      case 'Home':
        focusAt(0, ev.shiftKey)
        break
      case 'End':
        focusAt(entries.length - 1, ev.shiftKey)
        break
      case 'PageDown':
        focusAt(idx + pageSize, ev.shiftKey)
        break
      case 'PageUp':
        focusAt(idx - pageSize, ev.shiftKey)
        break
      case 'Enter':
        if (idx >= 0) open(entries[idx])
        break
      case 'Backspace':
        if (path) go(dirname(path))
        break
      case 'Escape':
        ui.clearSelection()
        break
      case 'F2':
        void rename()
        break
      case 'Delete':
        void remove()
        break
      case ' ':
        if (idx >= 0) select(entries[idx], { shiftKey: false, ctrlKey: true, metaKey: false })
        break
      default:
        if (mod && ev.key.toLowerCase() === 'a') ui.setSelection(new Set(entries.map((e) => e.name)))
        else if (mod && ev.key.toLowerCase() === 'c') clip('copy')
        else if (mod && ev.key.toLowerCase() === 'x') clip('cut')
        else if (mod && ev.key.toLowerCase() === 'v') void paste()
        else if (mod && ev.shiftKey && ev.key.toLowerCase() === 'n') void newFolder()
        else if (!mod && ev.key.length === 1) {
          const now = Date.now()
          const ta = typeahead.current
          ta.buf = now - ta.t > 700 ? ev.key : ta.buf + ev.key
          ta.t = now
          const q = ta.buf.toLowerCase()
          const start = ta.buf.length === 1 ? idx + 1 : 0
          const found = [...entries.slice(start), ...entries.slice(0, start)].find((e) => e.name.toLowerCase().startsWith(q))
          if (found) ui.setSelection(new Set([found.name]), found.name, found.name)
          break
        } else return
    }
    ev.preventDefault()
  }

  // ---- context menu ----
  const menuItems = (entry: Entry | null): MenuItem[] => {
    const sel = entry && !ui.selection.has(entry.name) ? [entry] : selectedEntries
    const one = sel.length === 1 ? sel[0] : null
    if (!entry && sel.length === 0) {
      return [
        { label: S.newFolder, icon: <IFolderPlus size={16} />, onClick: newFolder, shortcut: 'Ctrl+Shift+N' },
        { label: S.uploadFiles, icon: <IUpload size={16} />, onClick: () => fileInput.current?.click() },
        { label: S.uploadFolder, icon: <IUpload size={16} />, onClick: () => dirInput.current?.click() },
        { label: S.paste, icon: <IPaste size={16} />, onClick: () => paste(), disabled: !ui.clipboard, shortcut: 'Ctrl+V' },
        { separator: true, label: '' },
        { label: S.downloadZip, icon: <IDownload size={16} />, onClick: () => download([]) },
        { label: isFav ? S.removeFavorite : S.addFavorite, icon: <IStar size={16} filled={!!isFav} />, onClick: () => toggleFavorite() },
        { label: S.share, icon: <IShare size={16} />, onClick: () => setShare(path) },
        { separator: true, label: '' },
        { label: S.refresh, icon: <IRefresh size={16} />, onClick: refresh },
      ]
    }
    const items: MenuItem[] = [{ label: one?.type === 'dir' ? S.open : previewKind(one!) ? S.preview : S.download, icon: <IEye size={16} />, onClick: () => one && open(one), disabled: !one, shortcut: 'Enter' }]
    if (one?.type === 'dir') items.push({ label: S.paste, icon: <IPaste size={16} />, onClick: () => paste(join(path, one.name)), disabled: !ui.clipboard })
    items.push(
      { label: S.download, icon: <IDownload size={16} />, onClick: () => download(sel) },
      { separator: true, label: '' },
      { label: S.copy, icon: <ICopy size={16} />, onClick: () => clip('copy', sel), shortcut: 'Ctrl+C' },
      { label: S.cut, icon: <IScissors size={16} />, onClick: () => clip('cut', sel), shortcut: 'Ctrl+X' },
      { label: S.rename, icon: <IEdit size={16} />, onClick: () => rename(one!), disabled: !one, shortcut: 'F2' },
      { label: S.delete, icon: <ITrash size={16} />, onClick: () => remove(sel), danger: true, shortcut: 'Del' },
    )
    if (one?.type === 'dir') {
      const p = join(path, one.name)
      const f = favs.data?.favorites.find((x) => x.path === p)
      items.push({ separator: true, label: '' }, { label: f ? S.removeFavorite : S.addFavorite, icon: <IStar size={16} filled={!!f} />, onClick: () => toggleFavorite(p) }, { label: S.share, icon: <IShare size={16} />, onClick: () => setShare(p) })
    }
    return items
  }

  const onContextMenu = (entry: Entry | null, ev: MouseEvent) => {
    ev.preventDefault()
    if (entry && !ui.selection.has(entry.name)) ui.setSelection(new Set([entry.name]), entry.name, entry.name)
    setMenu({ x: ev.clientX, y: ev.clientY, entry })
  }

  const one = selectedEntries.length === 1 ? selectedEntries[0] : null
  const shareName = share !== null ? basename(share) || S.home : ''

  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      onDragOver={(ev) => {
        if (!ev.dataTransfer.types.includes('Files')) return
        ev.preventDefault()
        setDragOver(true)
      }}
      onDragLeave={(ev) => ev.currentTarget === ev.target && setDragOver(false)}
      onDrop={(ev) => onDrop(ev)}
    >
      {/* toolbar */}
      <div className="flex flex-wrap items-center gap-1 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <button className="btn-ghost !px-1.5" onClick={() => go(dirname(path))} disabled={!path} title={S.goUp}><IArrowUp size={16} /></button>
        <Breadcrumb path={path} base="/b" rootLabel={S.home} />
        <div className="ml-auto flex items-center gap-1">
          <input className="input !w-40 !py-1 text-sm" placeholder={S.search} value={ui.filter} onChange={(e) => ui.setFilter(e.target.value)} onKeyDown={(e) => e.key === 'Escape' && (ui.setFilter(''), listRef.current?.focus())} />
          <button className="btn-ghost !px-1.5" onClick={() => ui.setView(ui.view === 'list' ? 'grid' : 'list')} title={S.view}>{ui.view === 'list' ? <IGrid size={16} /> : <IList size={16} />}</button>
          <button className="btn-ghost !px-1.5" onClick={refresh} title={S.refresh}>{listing.isFetching ? <ISpinner size={16} /> : <IRefresh size={16} />}</button>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1 border-b border-neutral-200 px-3 py-1.5 dark:border-neutral-800">
        <button className="btn-ghost" onClick={newFolder}><IFolderPlus size={16} /> {S.newFolder}</button>
        <button className="btn-ghost" onClick={() => fileInput.current?.click()}><IUpload size={16} /> {S.uploadFiles}</button>
        <button className="btn-ghost" onClick={() => dirInput.current?.click()}><IUpload size={16} /> {S.uploadFolder}</button>
        <span className="mx-1 h-5 border-l border-neutral-300 dark:border-neutral-700" />
        <button className="btn-ghost" onClick={() => download()} title={S.download}><IDownload size={16} /> {S.download}</button>
        <button className="btn-ghost" onClick={() => clip('copy')} disabled={selectedEntries.length === 0}><ICopy size={16} /> {S.copy}</button>
        <button className="btn-ghost" onClick={() => clip('cut')} disabled={selectedEntries.length === 0}><IScissors size={16} /> {S.cut}</button>
        <button className="btn-ghost" onClick={() => paste()} disabled={!ui.clipboard}><IPaste size={16} /> {S.paste}{ui.clipboard && ` (${ui.clipboard.names.length})`}</button>
        <button className="btn-ghost" onClick={() => rename()} disabled={!one}><IEdit size={16} /> {S.rename}</button>
        <button className="btn-ghost text-red-600" onClick={() => remove()} disabled={selectedEntries.length === 0}><ITrash size={16} /> {S.delete}</button>
        <span className="mx-1 h-5 border-l border-neutral-300 dark:border-neutral-700" />
        <button className="btn-ghost" onClick={() => setShare(one?.type === 'dir' ? join(path, one.name) : path)} disabled={selectedEntries.length > 1 || (one !== null && one.type !== 'dir')}><IShare size={16} /> {S.share}</button>
        <button className="btn-ghost" onClick={() => toggleFavorite()} title={isFav ? S.removeFavorite : S.addFavorite}><IStar size={16} filled={!!isFav} className={isFav ? 'text-amber-500' : ''} /></button>
        <span className="ml-auto text-xs text-neutral-500">{selectedEntries.length > 0 ? S.selected(selectedEntries.length) : S.items(entries.length)}</span>
      </div>

      {uploads.pending.length > 0 && (
        <div className="flex items-center gap-2 bg-amber-100 px-3 py-1.5 text-sm text-amber-900 dark:bg-amber-900/40 dark:text-amber-100">
          <span className="flex-1">{S.pendingUploads(uploads.pending.length)}</span>
          <button className="btn-ghost !py-0.5 text-xs" onClick={() => uploadManager.discardPending()}>{S.discardPending}</button>
        </div>
      )}

      {/* list */}
      <div ref={listRef} tabIndex={0} className="flex min-h-0 flex-1 flex-col outline-none" onKeyDown={onKeyDown}>
        {listing.isLoading ? (
          <div className="flex flex-1 items-center justify-center text-neutral-400"><ISpinner /></div>
        ) : listing.error ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-red-600">
            <span>{errorMessage(listing.error.code, listing.error.message)}</span>
            <div className="flex gap-2">
              <button className="btn-ghost" onClick={() => go('')}>{S.home}</button>
              <button className="btn-ghost" onClick={refresh}>{S.retry}</button>
            </div>
          </div>
        ) : (
          <FileList
            entries={entries}
            selection={ui.selection}
            focused={ui.focused}
            cutNames={cutNames}
            view={ui.view}
            sort={ui.sort}
            onSort={ui.setSort}
            onRowClick={select}
            onOpen={open}
            onContextMenu={onContextMenu}
            onBackgroundClick={() => ui.clearSelection()}
            onDropOnEntry={(e, ev) => onDrop(ev, join(path, e.name))}
            dragOverName={dragOverName}
            onDragOverEntry={setDragOverName}
            emptyMessage={ui.filter ? S.notFound : S.emptyFolder}
          />
        )}
      </div>
      <div className="hidden border-t border-neutral-200 px-3 py-1 text-[11px] text-neutral-400 dark:border-neutral-800 md:block">{S.keyboardHint}</div>

      {dragOver && (
        <div className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-4 border-dashed border-blue-500 bg-blue-500/10 text-lg font-medium text-blue-700 dark:text-blue-200">
          {S.dropHere}
        </div>
      )}

      <input ref={fileInput} type="file" multiple hidden onChange={(e) => (addFiles(collectFromFileList(e.target.files ?? []), path), (e.target.value = ''))} />
      <input ref={dirInput} type="file" hidden {...({ webkitdirectory: '', directory: '' } as Record<string, string>)} onChange={(e) => (addFiles(collectFromFileList(e.target.files ?? []), path), (e.target.value = ''))} />

      {menu && <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.entry)} onClose={() => setMenu(null)} />}
      {preview !== null && entries[preview] && (
        <Preview entries={entries} index={preview} urlFor={(e, inline) => Api.contentUrl(join(path, e.name), inline)} maxText={cfg?.previewMaxText ?? 1 << 20} onClose={() => setPreview(null)} onIndex={setPreview} />
      )}
      {share !== null && <ShareDialog path={share} name={shareName} maxTtl={cfg?.shareMaxTtl ?? 30 * 86400} onClose={() => setShare(null)} onCreated={() => qc.invalidateQueries({ queryKey: ['shares'] })} />}
      {user === null && null}
    </div>
  )
}
