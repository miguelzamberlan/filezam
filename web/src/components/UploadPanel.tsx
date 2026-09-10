import { useSyncExternalStore, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { uploadManager, type UploadItem } from '../upload/manager'
import { useUI } from '../store/ui'
import { formatBytes, formatDuration, formatSpeed } from '../lib/format'
import { S, errorMessage } from '../strings'
import { IClose, IPause, IPlay, IChevronRight, IUpload, ICheck, IAlert } from './Icons'

export function useUploads() {
  return useSyncExternalStore(uploadManager.subscribe, uploadManager.getSnapshot)
}

function stateLabel(it: UploadItem) {
  switch (it.state) {
    case 'done':
      return <ICheck size={14} className="text-emerald-600" />
    case 'failed':
      return <span title={it.error}><IAlert size={14} className="text-red-600" /></span>
    case 'skipped':
      return <span className="text-xs text-neutral-400">{S.skip}</span>
    case 'cancelled':
      return <span className="text-xs text-neutral-400">{S.jobCancelled}</span>
    case 'conflict':
      return <span className="text-xs text-amber-600">?</span>
    default:
      return <span className="text-xs text-neutral-500">{it.size > 0 ? Math.floor((Math.min(it.sent, it.size) / it.size) * 100) + '%' : ''}</span>
  }
}

export default function UploadPanel() {
  const snap = useUploads()
  // aberto/recolhido vive no store para o item "Uploads" do menu poder expandir o painel
  const open = useUI((s) => s.uploadPanelOpen)
  const setOpen = useUI((s) => s.setUploadPanelOpen)
  const parentRef = useRef<HTMLDivElement>(null)
  const items = snap.items
  const rowVirtualizer = useVirtualizer({ count: items.length, getScrollElement: () => parentRef.current, estimateSize: () => 30, overscan: 10 })
  if (items.length === 0) return null
  const pct = snap.bytesTotal > 0 ? (snap.bytesDone / snap.bytesTotal) * 100 : 0
  const remaining = snap.speed > 0 ? (snap.bytesTotal - snap.bytesDone) / snap.speed : NaN
  const busy = snap.active > 0 || items.some((i) => i.state === 'queued' || i.state === 'uploading')
  return (
    <div className="card fixed bottom-3 right-3 z-40 w-[26rem] max-w-[calc(100vw-1.5rem)] overflow-hidden text-sm">
      <div className="flex items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <IUpload size={16} />
        <span className="font-medium">{S.uploadPanelTitle}</span>
        <span className="text-xs text-neutral-500">{snap.filesDone}/{snap.filesTotal}{snap.failed > 0 && ` · ${snap.failed} ${S.jobFailed.toLowerCase()}`}</span>
        <span className="ml-auto flex items-center gap-1">
          {busy && (
            <button className="btn-ghost !p-1" onClick={() => (snap.paused ? uploadManager.resume() : uploadManager.pause())} title={snap.paused ? S.resume : S.pause}>
              {snap.paused ? <IPlay size={14} /> : <IPause size={14} />}
            </button>
          )}
          {busy && <button className="btn-ghost !px-1.5 !py-0.5 text-xs" onClick={() => uploadManager.cancelAll()}>{S.cancelAll}</button>}
          {!busy && snap.failed > 0 && <button className="btn-ghost !px-1.5 !py-0.5 text-xs" onClick={() => uploadManager.retryFailed()}>{S.retryFailed}</button>}
          {!busy && <button className="btn-ghost !p-1" onClick={() => uploadManager.clearDone()} title={S.clearDone}><IClose size={14} /></button>}
          <button className="btn-ghost !p-1" onClick={() => setOpen(!open)}>
            <IChevronRight size={14} className={'transition ' + (open ? 'rotate-90' : '-rotate-90')} />
          </button>
        </span>
      </div>
      <div className="px-3 py-2">
        <div className="h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800">
          <div className={'h-full transition-[width] ' + (snap.paused ? 'bg-amber-500' : 'bg-accent')} style={{ width: pct + '%' }} />
        </div>
        <div className="mt-1 flex justify-between text-xs text-neutral-500">
          <span className="tabular-nums">{formatBytes(snap.bytesDone)} / {formatBytes(snap.bytesTotal)}</span>
          {busy && !snap.paused && <span className="tabular-nums">{formatSpeed(snap.speed)} · {formatDuration(remaining)} {S.uploadRemaining}</span>}
          {snap.paused && <span>{S.pause}</span>}
        </div>
      </div>
      {open && (
        <div ref={parentRef} className="max-h-56 overflow-auto border-t border-neutral-200 dark:border-neutral-800">
          <div style={{ height: rowVirtualizer.getTotalSize(), position: 'relative' }}>
            {rowVirtualizer.getVirtualItems().map((v) => {
              const it = items[v.index]
              return (
                <div key={it.id} className="absolute left-0 flex w-full items-center gap-2 px-3" style={{ top: v.start, height: v.size }}>
                  <span className="min-w-0 flex-1 truncate" title={it.relPath}>{it.relPath}</span>
                  {it.state === 'failed' && <span className="truncate text-xs text-red-600">{errorMessage(it.errorCode, it.error)}</span>}
                  <span className="w-16 text-right text-xs text-neutral-500">{formatBytes(it.size, 0)}</span>
                  <span className="w-10 text-right">{stateLabel(it)}</span>
                  {(it.state === 'queued' || it.state === 'uploading' || it.state === 'conflict') && (
                    <button className="btn-ghost !p-0.5" onClick={() => uploadManager.cancel(it.id)}><IClose size={12} /></button>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
