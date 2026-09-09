import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { Entry } from '../api/types'
import { formatRelative } from '../lib/format'
import { decodePath, encodePath, join, dirname } from '../lib/paths'
import { sortEntries, type Sort } from '../lib/naturalSort'
import { S } from '../strings'
import FileList from '../components/FileList'
import Breadcrumb from '../components/Breadcrumb'
import Preview, { previewKind } from '../components/Preview'
import ContextMenu from '../components/ContextMenu'
import { IDownload, ISpinner, IArrowUp, IEye } from '../components/Icons'

export default function PublicShare() {
  const { token = '', '*': rest = '' } = useParams()
  const path = decodePath(rest)
  const navigate = useNavigate()
  const info = useQuery({ queryKey: ['public', token], queryFn: () => Api.publicInfo(token), retry: false })
  const list = useQuery({ queryKey: ['public', token, 'list', path], queryFn: () => Api.publicList(token, path), enabled: info.isSuccess, retry: false })
  const [sort, setSort] = useState<Sort>({ key: 'name', dir: 'asc' })
  const [selection, setSelection] = useState<Set<string>>(new Set())
  const [focused, setFocused] = useState<string | null>(null)
  const [preview, setPreview] = useState<number | null>(null)
  const [menu, setMenu] = useState<{ x: number; y: number; entry: Entry | null } | null>(null)
  const entries = useMemo(() => sortEntries(list.data?.entries ?? [], sort), [list.data, sort])
  useEffect(() => {
    setSelection(new Set())
    setPreview(null)
  }, [path])
  useEffect(() => {
    if (info.data) document.title = info.data.name + ' · ' + S.appName
  }, [info.data])

  const go = (p: string) => navigate(`/s/${token}` + (p ? '/' + encodePath(p) : ''))
  const urlFor = (e: Entry, inline: boolean) => Api.publicContentUrl(token, join(path, e.name), inline)
  const open = (e: Entry) => {
    if (e.type === 'dir') return go(join(path, e.name))
    if (e.type !== 'file') return
    if (previewKind(e)) setPreview(entries.indexOf(e))
    else window.location.href = urlFor(e, false)
  }
  const select = (e: Entry, ev: MouseEvent) => {
    const s = new Set(ev.ctrlKey || ev.metaKey ? selection : [])
    if (s.has(e.name)) s.delete(e.name)
    else s.add(e.name)
    setSelection(s)
    setFocused(e.name)
  }
  const downloadSel = () => {
    const sel = entries.filter((e) => selection.has(e.name))
    if (sel.length === 1 && sel[0].type === 'file') window.location.href = urlFor(sel[0], false)
    else if (sel.length === 0) window.location.href = Api.publicZipUrl(token, [path])
    else window.location.href = Api.publicZipUrl(token, sel.map((e) => join(path, e.name)))
  }

  if (info.isLoading) return <div className="flex h-full items-center justify-center"><ISpinner /></div>
  if (info.error) {
    return (
      <div className="flex h-full items-center justify-center p-4">
        <div className="card max-w-md p-6 text-center">
          <h1 className="text-lg font-semibold">{S.appName}</h1>
          <p className="mt-3 text-sm text-neutral-600 dark:text-neutral-400">{info.error instanceof ApiError && info.error.status === 429 ? S.errorCodes.rate_limited : S.publicNotFound}</p>
        </div>
      </div>
    )
  }
  const one = entries.filter((e) => selection.has(e.name))
  return (
    <div className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <img src="/favicon.svg" alt="" className="h-6 w-6" />
        <button className="btn-ghost !px-1.5" onClick={() => go(dirname(path))} disabled={!path}><IArrowUp size={16} /></button>
        <Breadcrumb path={path} base="" rootLabel={info.data?.name ?? ''} onNavigate={go} />
        <div className="ml-auto flex items-center gap-2 text-xs text-neutral-500">
          <span>{S.shareReadOnly} · {S.shareExpiresAt} {info.data && formatRelative(info.data.expiresAt)}</span>
          <button className="btn-primary" onClick={downloadSel}><IDownload size={16} /> {selection.size > 0 ? S.download : S.downloadZip}</button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col">
        {list.isLoading ? (
          <div className="flex flex-1 items-center justify-center"><ISpinner /></div>
        ) : list.error ? (
          <div className="flex flex-1 items-center justify-center text-sm text-red-600">{S.notFound}</div>
        ) : (
          <FileList
            entries={entries}
            selection={selection}
            focused={focused}
            view="list"
            sort={sort}
            onSort={setSort}
            onRowClick={select}
            onOpen={open}
            onContextMenu={(entry, ev) => {
              ev.preventDefault()
              if (entry) setSelection(new Set([entry.name]))
              setMenu({ x: ev.clientX, y: ev.clientY, entry })
            }}
            onBackgroundClick={() => setSelection(new Set())}
            readOnly
          />
        )}
      </div>
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: menu.entry?.type === 'dir' ? S.open : S.preview, icon: <IEye size={16} />, onClick: () => menu.entry && open(menu.entry), disabled: !menu.entry },
            { label: S.download, icon: <IDownload size={16} />, onClick: downloadSel },
          ]}
        />
      )}
      {preview !== null && entries[preview] && (
        <Preview entries={entries} index={preview} urlFor={urlFor} maxText={1 << 20} onClose={() => setPreview(null)} onIndex={setPreview} />
      )}
      {one.length === 0 && null}
    </div>
  )
}
