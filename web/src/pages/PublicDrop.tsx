import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type DragEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { PublicInfo } from '../api/types'
import { formatBytes, formatRelative } from '../lib/format'
import { S, errorMessage } from '../strings'
import { DropUploader } from '../upload/dropUploader'
import { ICheck, IAlert, IUpload, ISpinner, IClose, IRefresh, iconFor } from '../components/Icons'

/**
 * Página de um link público de recebimento: só envia. Não lista, não baixa e não mostra o que
 * outras pessoas mandaram — o visitante vê apenas os próprios envios, identificados por um
 * cookie assinado pelo servidor.
 */
export default function PublicDrop({ token, info }: { token: string; info: PublicInfo }) {
  const qc = useQueryClient()
  const up = useMemo(() => new DropUploader(), [])
  const snap = useSyncExternalStore(up.subscribe, up.getSnapshot)
  const [dragOver, setDragOver] = useState(false)
  const picker = useRef<HTMLInputElement>(null)

  useEffect(() => {
    up.configure(token, { chunkSize: info.chunkSize ?? 16 << 20, maxParallel: info.maxParallel ?? 2, maxFileBytes: info.maxFileBytes ?? 0 })
    // Cada arquivo concluído atualiza cota, contagem e a lista de envios, que vêm do servidor.
    up.onDone = () => qc.invalidateQueries({ queryKey: ['public', token] })
  }, [up, token, info.chunkSize, info.maxParallel, info.maxFileBytes, qc])

  const quota = info.quotaBytes ?? 0
  const used = info.usedBytes ?? 0
  const left = Math.max(quota - used, 0)
  const filesLeft = Math.max((info.maxFiles ?? 0) - (info.fileCount ?? 0), 0)
  const mine = info.mine ?? []

  // Só arquivos soltos: o link é plano, sem estrutura de pastas, então uma pasta arrastada é
  // ignorada em vez de virar um monte de nomes achatados que colidem entre si.
  const onDrop = (ev: DragEvent) => {
    ev.preventDefault()
    setDragOver(false)
    const files: File[] = []
    for (const item of Array.from(ev.dataTransfer.items ?? [])) {
      const entry = item.webkitGetAsEntry?.()
      if (entry && !entry.isFile) continue
      const f = item.getAsFile()
      if (f) files.push(f)
    }
    if (files.length === 0) files.push(...Array.from(ev.dataTransfer.files))
    up.add(files)
  }

  return (
    <div className="mx-auto flex h-full w-full max-w-2xl flex-col gap-4 overflow-auto p-4">
      <div className="flex items-center gap-2">
        <img src="/favicon.svg" alt="" className="h-6 w-6" />
        <h1 className="truncate text-lg font-semibold">{info.name}</h1>
        <span className="ml-auto text-xs text-neutral-500">{S.expires} {formatRelative(info.expiresAt)}</span>
      </div>
      <p className="text-sm text-neutral-600 dark:text-neutral-400">{S.dropIntro}</p>

      <div className="card p-4">
        <div className="flex items-baseline justify-between text-xs text-neutral-500">
          <span>{S.dropSpaceLeft(formatBytes(left), formatBytes(quota))}</span>
          {!!info.maxFiles && <span>{S.dropFilesLeft(filesLeft, info.maxFiles)}</span>}
        </div>
        <div className="meter mt-2"><div className="meter-fill" style={{ width: `${quota > 0 ? Math.min(100, (used / quota) * 100) : 0}%` }} /></div>
        {!!info.maxFileBytes && <p className="mt-2 text-xs text-neutral-500">{S.dropFileMax}: {formatBytes(info.maxFileBytes)}</p>}
      </div>

      <div
        className={'dropzone' + (dragOver ? ' dropzone-active' : '')}
        onDragOver={(e) => { e.preventDefault(); setDragOver(true) }}
        onDragLeave={() => setDragOver(false)}
        onDrop={onDrop}
      >
        <IUpload size={28} />
        <p>{dragOver ? S.dropHere : S.dropZoneIdle}</p>
        <button className="btn-primary mt-1" onClick={() => picker.current?.click()}>{S.dropPick}</button>
        <input
          ref={picker}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => { up.add(Array.from(e.target.files ?? [])); e.target.value = '' }}
        />
      </div>

      {snap.items.length > 0 && (
        <div className="card divide-y divide-neutral-200 dark:divide-neutral-800">
          {snap.items.map((it) => (
            <div key={it.id} className="flex items-center gap-3 px-3 py-2 text-sm">
              <span className="shrink-0">{iconFor(it.name, 'file')}</span>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline gap-2">
                  <span className="truncate" title={it.name}>{it.name}</span>
                  <span className="ml-auto shrink-0 text-xs text-neutral-500">{formatBytes(it.size)}</span>
                </div>
                {it.state === 'uploading' && (
                  <div className="meter mt-1"><div className="meter-fill" style={{ width: `${it.size > 0 ? (it.sent / it.size) * 100 : 0}%` }} /></div>
                )}
                {it.state === 'failed' && <p className="mt-0.5 text-xs text-red-600">{errorMessage(it.errorCode)}</p>}
              </div>
              <span className="shrink-0">
                {it.state === 'done' ? <span className="text-emerald-600"><ICheck size={16} /></span>
                  : it.state === 'failed' ? <button className="btn-ghost !px-1.5" title={S.retryFailed} onClick={() => up.retry(it.id)}><IRefresh size={16} /></button>
                  : it.state === 'uploading' ? <ISpinner />
                  : <button className="btn-ghost !px-1.5" title={S.cancel} onClick={() => up.cancel(it.id)}><IClose size={16} /></button>}
              </span>
            </div>
          ))}
          {snap.items.some((i) => i.state === 'done') && (
            <div className="px-3 py-2 text-right">
              <button className="btn-ghost text-xs" onClick={() => up.clearDone()}>{S.clearDone}</button>
            </div>
          )}
        </div>
      )}

      <div>
        <h2 className="mb-2 text-sm font-semibold">{S.dropMine}</h2>
        {mine.length === 0 ? (
          <p className="text-sm text-neutral-500">{S.dropNothingYet}</p>
        ) : (
          <ul className="card divide-y divide-neutral-200 text-sm dark:divide-neutral-800">
            {mine.map((m) => (
              <li key={m.name + m.at} className="flex items-center gap-3 px-3 py-2">
                <span className="shrink-0">{iconFor(m.name, 'file')}</span>
                <span className="truncate" title={m.name}>{m.name}</span>
                <span className="ml-auto shrink-0 text-xs text-neutral-500">{formatBytes(m.size)} · {formatRelative(m.at)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <p className="flex items-start gap-2 text-xs text-neutral-500">
        <span className="mt-0.5 shrink-0"><IAlert size={14} /></span>
        {S.dropWriteOnly}
      </p>
    </div>
  )
}
