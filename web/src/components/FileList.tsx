import { useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { Entry } from '../api/types'
import { formatBytes, formatDate } from '../lib/format'
import type { Sort, SortKey } from '../lib/naturalSort'
import { S } from '../strings'
import { iconFor } from './Icons'

export interface FileListProps {
  entries: Entry[]
  selection: Set<string>
  focused: string | null
  cutNames?: Set<string>
  view: 'list' | 'grid'
  sort: Sort
  onSort: (s: Sort) => void
  onRowClick: (e: Entry, ev: MouseEvent) => void
  onOpen: (e: Entry) => void
  onContextMenu: (e: Entry | null, ev: MouseEvent) => void
  onBackgroundClick: () => void
  onDropOnEntry?: (e: Entry, ev: React.DragEvent) => void
  dragOverName?: string | null
  onDragOverEntry?: (name: string | null) => void
  emptyMessage?: string
  readOnly?: boolean
}

const ROW = 34

export default function FileList(props: FileListProps) {
  const { entries, selection, focused, cutNames, view, sort, onSort } = props
  const parentRef = useRef<HTMLDivElement>(null)
  const [cols, setCols] = useState(4)

  useLayoutEffect(() => {
    if (view !== 'grid') return
    const el = parentRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setCols(Math.max(2, Math.floor(el.clientWidth / 150))))
    ro.observe(el)
    return () => ro.disconnect()
  }, [view])

  const rows = view === 'grid' ? Math.ceil(entries.length / cols) : entries.length
  const virt = useVirtualizer({
    count: rows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => (view === 'grid' ? 130 : ROW),
    overscan: 12,
  })

  // keep focused row visible
  useEffect(() => {
    if (!focused) return
    const i = entries.findIndex((e) => e.name === focused)
    if (i >= 0) virt.scrollToIndex(view === 'grid' ? Math.floor(i / cols) : i, { align: 'auto' })
  }, [focused, entries, virt, view, cols])

  const header = (key: SortKey, label: string, cls: string) => (
    <button
      className={'flex items-center gap-1 px-2 py-1.5 text-left text-xs font-medium uppercase tracking-wide text-neutral-500 hover:text-neutral-800 dark:hover:text-neutral-200 ' + cls}
      onClick={() => onSort({ key, dir: sort.key === key && sort.dir === 'asc' ? 'desc' : 'asc' })}
    >
      {label} {sort.key === key && <span>{sort.dir === 'asc' ? '▲' : '▼'}</span>}
    </button>
  )

  const rowCls = useCallback(
    (e: Entry) =>
      'group flex select-none items-center rounded-sm ' +
      (selection.has(e.name) ? 'row-selected ' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800/60 ') +
      (focused === e.name ? 'row-focused ' : '') +
      (cutNames?.has(e.name) ? 'opacity-50 ' : '') +
      (props.dragOverName === e.name && e.type === 'dir' ? 'ring-2 ring-blue-500 ' : ''),
    [selection, focused, cutNames, props.dragOverName],
  )

  const dragProps = (e: Entry) =>
    e.type === 'dir' && props.onDropOnEntry
      ? {
          onDragOver: (ev: React.DragEvent) => {
            if (!ev.dataTransfer.types.includes('Files')) return
            ev.preventDefault()
            ev.stopPropagation()
            props.onDragOverEntry?.(e.name)
          },
          onDragLeave: () => props.onDragOverEntry?.(null),
          onDrop: (ev: React.DragEvent) => {
            ev.preventDefault()
            ev.stopPropagation()
            props.onDragOverEntry?.(null)
            props.onDropOnEntry?.(e, ev)
          },
        }
      : {}

  if (entries.length === 0) {
    return (
      <div ref={parentRef} className="flex flex-1 items-center justify-center text-sm text-neutral-400" onContextMenu={(ev) => props.onContextMenu(null, ev)} onClick={props.onBackgroundClick}>
        {props.emptyMessage ?? S.emptyFolder}
      </div>
    )
  }

  if (view === 'grid') {
    return (
      <div ref={parentRef} className="flex-1 overflow-auto p-2" onClick={(ev) => ev.target === ev.currentTarget && props.onBackgroundClick()} onContextMenu={(ev) => ev.target === ev.currentTarget && props.onContextMenu(null, ev)}>
        <div style={{ height: virt.getTotalSize(), position: 'relative' }}>
          {virt.getVirtualItems().map((v) => (
            <div key={v.key} className="absolute left-0 grid w-full gap-2" style={{ top: v.start, height: v.size, gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
              {entries.slice(v.index * cols, v.index * cols + cols).map((e) => (
                <div
                  key={e.name}
                  className={rowCls(e) + ' flex-col justify-center gap-1 p-2 text-center'}
                  onClick={(ev) => props.onRowClick(e, ev)}
                  onDoubleClick={() => props.onOpen(e)}
                  onContextMenu={(ev) => props.onContextMenu(e, ev)}
                  title={e.name}
                  {...dragProps(e)}
                >
                  <div className="flex h-14 items-center justify-center">{iconFor(e.name, e.type, 40)}</div>
                  <div className="w-full truncate text-xs">{e.name}</div>
                  <div className="text-[10px] text-neutral-500">{e.type === 'dir' ? S.folder : formatBytes(e.size)}</div>
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex border-b border-neutral-200 pr-2 dark:border-neutral-800">
        <div className="w-8" />
        {header('name', S.name, 'flex-1')}
        {header('size', S.size, 'w-24 justify-end')}
        {header('mtime', S.modified, 'hidden w-40 sm:flex')}
      </div>
      <div ref={parentRef} className="flex-1 overflow-auto" onClick={(ev) => ev.target === ev.currentTarget && props.onBackgroundClick()} onContextMenu={(ev) => ev.target === ev.currentTarget && props.onContextMenu(null, ev)}>
        <div style={{ height: virt.getTotalSize(), position: 'relative' }} onClick={(ev) => ev.target === ev.currentTarget && props.onBackgroundClick()} onContextMenu={(ev) => ev.target === ev.currentTarget && props.onContextMenu(null, ev)}>
          {virt.getVirtualItems().map((v) => {
            const e = entries[v.index]
            return (
              <div
                key={e.name}
                className={rowCls(e) + ' absolute left-0 w-full px-1'}
                style={{ top: v.start, height: v.size }}
                onClick={(ev) => props.onRowClick(e, ev)}
                onDoubleClick={() => props.onOpen(e)}
                onContextMenu={(ev) => props.onContextMenu(e, ev)}
                {...dragProps(e)}
              >
                <div className="flex w-8 shrink-0 items-center justify-center">{iconFor(e.name, e.type)}</div>
                <div className={'min-w-0 flex-1 truncate px-2 text-sm ' + (e.nameInvalid ? 'text-red-500' : '')} title={e.name}>
                  {e.name}
                  {e.link && <span className="ml-1 text-xs text-neutral-400">↗</span>}
                </div>
                <div className="w-24 shrink-0 px-2 text-right text-xs tabular-nums text-neutral-500">{e.type === 'dir' ? '—' : formatBytes(e.size)}</div>
                <div className="hidden w-40 shrink-0 px-2 text-xs tabular-nums text-neutral-500 sm:block">{formatDate(e.mtime)}</div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
