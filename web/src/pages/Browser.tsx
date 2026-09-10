import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent, type MouseEvent } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { Conflict, Entry } from '../api/types'
import { useAuth, useFavorites, useInvalidateDirs, useListing } from '../hooks'
import { basename, decodePath, dirname, encodePath, join } from '../lib/paths'
import { sortEntries } from '../lib/naturalSort'
import { useUI, ZOOM_STEPS } from '../store/ui'
import { useJobs } from '../store/jobs'
import { S, errorMessage } from '../strings'
import { uploadManager } from '../upload/manager'
import { collectFromDataTransfer, collectFromFileList, type PickedFile } from '../upload/walk'
import FileList, { DRAG_MIME, setDragging } from '../components/FileList'
import Breadcrumb from '../components/Breadcrumb'
import ContextMenu, { type MenuItem } from '../components/ContextMenu'
import Preview, { previewKind } from '../components/Preview'
import ShareDialog from '../components/ShareDialog'
import InfoDialog from '../components/InfoDialog'
import { dialogs, toast } from '../components/dialogs'
import { useUploads } from '../components/UploadPanel'
import { MenuButton } from '../components/Shell'
import {
  IArrowUp, ICopy, IDownload, IEdit, IFolderPlus, IGrid, IList, IPaste, IRefresh, IScissors, IShare, IStar, ITrash, IUpload, ISpinner, IEye, IInfo, ISearch, IZoomIn, IZoomOut, IMore, IClose,
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
  const [searchParams, setSearchParams] = useSearchParams()
  const qc = useQueryClient()
  const { user } = useAuth()
  const ui = useUI()
  const listing = useListing(path, ui.sort, ui.prefs.showHidden)
  const invalidate = useInvalidateDirs()
  const favs = useFavorites()
  const track = useJobs((s) => s.track)
  const uploads = useUploads()
  const cfg = useQuery({ queryKey: ['config'], queryFn: () => Api.config(), staleTime: Infinity }).data
  const [menu, setMenu] = useState<{ x: number; y: number; entry: Entry | null } | null>(null)
  const [preview, setPreview] = useState<number | null>(null)
  const [share, setShare] = useState<string | null>(null)
  const [info, setInfo] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [dragOverName, setDragOverName] = useState<string | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const dirInput = useRef<HTMLInputElement>(null)
  const typeahead = useRef({ buf: '', t: 0 })

  // O servidor já filtrou ocultos e ordenou. Com tudo carregado, reordena com o Intl.Collator
  // do navegador (mais fiel ao idioma); com páginas ainda por vir, mantém a ordem do servidor
  // para as páginas não se embaralharem ao chegar.
  const paged = !!listing.data && listing.data.entries.length < listing.data.total
  const entries = useMemo(() => {
    const all = listing.data?.entries ?? []
    const f = ui.filter.trim().toLowerCase()
    const filtered = f ? all.filter((e) => e.name.toLowerCase().includes(f)) : all
    return paged ? filtered : sortEntries(filtered, ui.sort)
  }, [listing.data, ui.filter, ui.sort, paged])
  const hiddenCount = listing.data?.hidden ?? 0
  const total = listing.data?.total ?? 0

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

  // ?sel=nome (vindo da pesquisa): seleciona o item assim que a listagem chega e limpa o parâmetro
  useEffect(() => {
    const sel = searchParams.get('sel')
    if (!sel || !listing.data) return
    if (listing.data.entries.some((e) => e.name === sel)) ui.setSelection(new Set([sel]), sel, sel)
    setSearchParams({}, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listing.data, searchParams])

  const zoomStep = (dir: 1 | -1) => {
    const i = ZOOM_STEPS.indexOf(ui.prefs.zoom)
    const next = ZOOM_STEPS[Math.max(0, Math.min(ZOOM_STEPS.length - 1, (i < 0 ? 1 : i) + dir))]
    ui.setPrefs({ zoom: next })
  }

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

  // No toque: um toque abre (pasta ou arquivo); com uma seleção ativa (iniciada por toque
  // longo), o toque alterna a seleção. Com mouse: clique seleciona, duplo clique abre.
  const rowClick = (e: Entry, ev: MouseEvent) => {
    const touch = (ev.nativeEvent as PointerEvent).pointerType === 'touch'
    if (!touch) return select(e, ev)
    if (ui.selection.size === 0) return open(e)
    select(e, { shiftKey: false, ctrlKey: true, metaKey: false })
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

  // Excluir vai para a lixeira quando o servidor a mantém (trashRetention > 0); permanent
  // (Shift+Del ou o item do menu) apaga de vez. Com a lixeira ativa a confirmação é mais leve.
  const trashOn = (cfg?.trashRetention ?? 0) > 0
  const remove = async (list = selectedEntries, permanent = false) => {
    if (list.length === 0) return
    const toTrash = trashOn && !permanent
    if (toTrash ? ui.prefs.confirmDelete : true) {
      const ok = await dialogs.confirm(
        toTrash
          ? { title: S.moveToTrashConfirm(list.length), message: (list.length === 1 ? list[0].name + ' — ' : '') + S.moveToTrashHint, okLabel: S.delete }
          : { title: S.deleteConfirm(list.length), message: list.length === 1 ? list[0].name : S.deleteWarning, danger: true, okLabel: S.deleteForever, requireCheck: S.confirmIrreversible },
      )
      if (!ok) return
    }
    try {
      const { job } = await Api.delete(list.map((e) => join(path, e.name)), !toTrash)
      track(job)
      ui.clearSelection()
      qc.invalidateQueries({ queryKey: ['favorites'] })
      qc.invalidateQueries({ queryKey: ['trash'] })
      if (toTrash) toast(S.movedToTrash(list.length), 'success')
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

  // ---- arrastar e soltar interno (mover para uma pasta da listagem ou da trilha) ----
  const dragStart = (e: Entry, ev: DragEvent) => {
    const names = ui.selection.has(e.name) ? [...ui.selection] : [e.name]
    if (!ui.selection.has(e.name)) ui.setSelection(new Set([e.name]), e.name, e.name)
    ev.dataTransfer.setData(DRAG_MIME, JSON.stringify(names))
    ev.dataTransfer.effectAllowed = 'move'
    setDragging(names)
  }
  const moveInto = async (destDir: string, names: string[]) => {
    names = names.filter((n) => byName.has(n))
    if (names.length === 0 || destDir === path) return
    if (names.some((n) => destDir === join(path, n) || destDir.startsWith(join(path, n) + '/'))) return toast(S.errorCodes.nested, 'error')
    let policy: Conflict = 'rename'
    try {
      const dest = await Api.list(destDir)
      const taken = new Set(dest.entries.map((e) => e.name))
      const clash = names.find((n) => taken.has(n))
      if (clash) {
        const ans = await dialogs.conflict(clash)
        if (ans.choice === 'cancel') return
        policy = ans.choice
      }
      const { job } = await Api.move(names.map((n) => join(path, n)), destDir, policy)
      track(job)
      ui.clearSelection()
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
    if (menu || preview !== null || share || info !== null) return
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
    const pageSize = Math.max(1, Math.floor((listRef.current?.clientHeight ?? 400) / (34 * ui.prefs.zoom)) - 1)
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
        void remove(selectedEntries, ev.shiftKey)
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
    const header: MenuItem = { label: '/' + (one ? join(path, one.name) : path), disabled: true, icon: <IInfo size={14} /> }
    if (!entry && sel.length === 0) {
      return [
        header,
        { label: S.properties, icon: <IInfo size={16} />, onClick: () => setInfo(path) },
        { separator: true, label: '' },
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
    const items: MenuItem[] = [
      ...(one ? [header] : []),
      { label: one?.type === 'dir' ? S.open : previewKind(one!) ? S.preview : S.download, icon: <IEye size={16} />, onClick: () => one && open(one), disabled: !one, shortcut: 'Enter' },
    ]
    if (one?.type === 'dir') items.push({ label: S.paste, icon: <IPaste size={16} />, onClick: () => paste(join(path, one.name)), disabled: !ui.clipboard })
    items.push(
      { label: S.download, icon: <IDownload size={16} />, onClick: () => download(sel) },
      { separator: true, label: '' },
      { label: S.copy, icon: <ICopy size={16} />, onClick: () => clip('copy', sel), shortcut: 'Ctrl+C' },
      { label: S.cut, icon: <IScissors size={16} />, onClick: () => clip('cut', sel), shortcut: 'Ctrl+X' },
      { label: S.rename, icon: <IEdit size={16} />, onClick: () => rename(one!), disabled: !one, shortcut: 'F2' },
      { label: S.delete, icon: <ITrash size={16} />, onClick: () => remove(sel), danger: true, shortcut: 'Del' },
      ...(trashOn ? [{ label: S.deleteForever, icon: <ITrash size={16} />, onClick: () => remove(sel, true), danger: true, shortcut: 'Shift+Del' } as MenuItem] : []),
      { separator: true, label: '' },
      { label: S.properties, icon: <IInfo size={16} />, onClick: () => one && setInfo(join(path, one.name)), disabled: !one },
    )
    if (one?.type === 'dir') {
      const p = join(path, one.name)
      const f = favs.data?.favorites.find((x) => x.path === p)
      items.push({ separator: true, label: '' }, { label: f ? S.removeFavorite : S.addFavorite, icon: <IStar size={16} filled={!!f} />, onClick: () => toggleFavorite(p) }, { label: S.share, icon: <IShare size={16} />, onClick: () => setShare(p) })
    } else if (one?.type === 'file') {
      items.push({ separator: true, label: '' }, { label: S.share, icon: <IShare size={16} />, onClick: () => setShare(join(path, one.name)) })
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
      {/* toolbar: linha 1 = navegação; linha 2 = ações (completa em telas ≥ sm, compacta com "mais" no celular) */}
      <div className="flex items-center gap-1 border-b border-neutral-200 px-2 py-2 sm:px-3 dark:border-neutral-800">
        <MenuButton />
        <button className="btn-ghost !px-1.5" onClick={() => go(dirname(path))} disabled={!path} title={S.goUp}><IArrowUp size={16} /></button>
        <div className="min-w-0 flex-1"><Breadcrumb path={path} base="/b" rootLabel={S.home} onDropNames={moveInto} /></div>
        <div className="flex shrink-0 items-center gap-1">
          <input className="input hidden !w-40 !py-1 text-sm sm:block" placeholder={S.search} value={ui.filter} onChange={(e) => ui.setFilter(e.target.value)} onKeyDown={(e) => e.key === 'Escape' && (ui.setFilter(''), listRef.current?.focus())} />
          <button className="btn-ghost !px-1.5" onClick={() => navigate('/search?path=' + encodeURIComponent(path))} title={S.searchHere}><ISearch size={16} /></button>
          <span className="mx-1 hidden h-5 border-l border-neutral-300 sm:block dark:border-neutral-700" />
          <button className="btn-ghost hidden !px-1.5 sm:inline-flex" onClick={() => zoomStep(-1)} disabled={ui.prefs.zoom <= ZOOM_STEPS[0]} title={S.zoomOut}><IZoomOut size={16} /></button>
          <button className="btn-ghost hidden !px-1 text-xs tabular-nums sm:inline-flex" onClick={() => ui.setPrefs({ zoom: 1 })} title={S.zoomReset}>{Math.round(ui.prefs.zoom * 100)}%</button>
          <button className="btn-ghost hidden !px-1.5 sm:inline-flex" onClick={() => zoomStep(1)} disabled={ui.prefs.zoom >= ZOOM_STEPS[ZOOM_STEPS.length - 1]} title={S.zoomIn}><IZoomIn size={16} /></button>
          <button className="btn-ghost !px-1.5" onClick={() => ui.setView(ui.view === 'list' ? 'grid' : 'list')} title={S.view}>{ui.view === 'list' ? <IGrid size={16} /> : <IList size={16} />}</button>
          <button className="btn-ghost hidden !px-1.5 sm:inline-flex" onClick={refresh} title={S.refresh}>{listing.isFetching ? <ISpinner size={16} /> : <IRefresh size={16} />}</button>
        </div>
      </div>
      {/* celular: filtro + ações principais + "mais" (o resto vai para o menu de contexto) */}
      <div className="flex items-center gap-1 border-b border-neutral-200 px-2 py-1.5 sm:hidden dark:border-neutral-800">
        {selectedEntries.length > 0 ? (
          <button className="btn-ghost !px-1.5 text-xs" onClick={() => ui.clearSelection()} title={S.clearSelection}><IClose size={14} /> {S.selected(selectedEntries.length)}</button>
        ) : (
          <>
            <button className="btn-ghost !px-1.5" onClick={newFolder} title={S.newFolder}><IFolderPlus size={18} /></button>
            <button className="btn-ghost !px-1.5" onClick={() => fileInput.current?.click()} title={S.uploadFiles}><IUpload size={18} /></button>
          </>
        )}
        <input className="input min-w-0 flex-1 !py-1 text-sm" placeholder={S.search} value={ui.filter} onChange={(e) => ui.setFilter(e.target.value)} />
        {ui.clipboard && selectedEntries.length === 0 && <button className="btn-ghost !px-1.5" onClick={() => paste()} title={S.paste}><IPaste size={18} /></button>}
        <button className="btn-ghost !px-1.5" title={S.more} onClick={(ev) => { const r = ev.currentTarget.getBoundingClientRect(); setMenu({ x: r.right - 220, y: r.bottom + 4, entry: selectedEntries[0] ?? null }) }}><IMore size={18} /></button>
      </div>
      <div className="hidden flex-wrap items-center gap-1 border-b border-neutral-200 px-3 py-1.5 sm:flex dark:border-neutral-800">
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
        <button className="btn-ghost" onClick={() => setShare(one ? join(path, one.name) : path)} disabled={selectedEntries.length > 1 || (one !== null && one.type === 'other')}><IShare size={16} /> {S.share}</button>
        <button className="btn-ghost" onClick={() => toggleFavorite()} title={isFav ? S.removeFavorite : S.addFavorite}><IStar size={16} filled={!!isFav} className={isFav ? 'text-amber-500' : ''} /></button>
        <button className="btn-ghost" onClick={() => setInfo(one ? join(path, one.name) : path)} disabled={selectedEntries.length > 1} title={S.properties}><IInfo size={16} /></button>
        <span className="ml-auto text-xs text-neutral-500">
          {selectedEntries.length > 0 ? S.selected(selectedEntries.length) : ui.filter ? `${entries.length} / ${S.items(total)}` : S.items(total)}
          {paged && <span> · {S.loadedOf(listing.data!.entries.length, total)}</span>}
          {hiddenCount > 0 && <span title={S.prefShowHidden}> · {hiddenCount} ocultos</span>}
        </span>
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
            onRowClick={rowClick}
            onOpen={open}
            onContextMenu={onContextMenu}
            onBackgroundClick={() => ui.clearSelection()}
            onDropOnEntry={(e, ev) => onDrop(ev, join(path, e.name))}
            onDragStartEntry={dragStart}
            onEndReached={() => listing.hasNextPage && !listing.isFetchingNextPage && listing.fetchNextPage()}
            onDropEntries={(target, names) => moveInto(join(path, target.name), names)}
            dragOverName={dragOverName}
            onDragOverEntry={setDragOverName}
            emptyMessage={ui.filter ? S.notFound : S.emptyFolder}
            zoom={ui.prefs.zoom}
          />
        )}
      </div>
      {ui.prefs.showHints && <div className="hidden border-t border-neutral-200 px-3 py-1 text-[11px] text-neutral-400 dark:border-neutral-800 md:block">{S.keyboardHint}</div>}

      {dragOver && (
        <div className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-4 border-dashed border-accent bg-accent/10 text-lg font-medium text-accent">
          {S.dropHere}
        </div>
      )}

      <input ref={fileInput} type="file" multiple hidden onChange={(e) => (addFiles(collectFromFileList(e.target.files ?? []), path), (e.target.value = ''))} />
      <input ref={dirInput} type="file" hidden {...({ webkitdirectory: '', directory: '' } as Record<string, string>)} onChange={(e) => (addFiles(collectFromFileList(e.target.files ?? []), path), (e.target.value = ''))} />

      {menu && <ContextMenu x={menu.x} y={menu.y} items={menuItems(menu.entry)} onClose={() => setMenu(null)} />}
      {preview !== null && entries[preview] && (
        <Preview entries={entries} index={preview} urlFor={(e, inline) => Api.contentUrl(join(path, e.name), inline)} maxText={cfg?.previewMaxText ?? 1 << 20} assetUrl={(p) => Api.contentUrl(join(path, p), true)} onClose={() => setPreview(null)} onIndex={setPreview} />
      )}
      {info !== null && <InfoDialog path={info} onClose={() => setInfo(null)} />}
      {share !== null && <ShareDialog path={share} name={shareName} kind={share !== path && byName.get(basename(share))?.type === 'file' ? 'file' : 'dir'} maxTtl={cfg?.shareMaxTtl ?? 30 * 86400} onClose={() => setShare(null)} onCreated={() => qc.invalidateQueries({ queryKey: ['shares'] })} />}
      {user === null && null}
    </div>
  )
}
