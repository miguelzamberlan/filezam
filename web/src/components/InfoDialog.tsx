import { useQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import { formatBytes, formatDate, formatRelative } from '../lib/format'
import { S } from '../strings'
import { Modal, toast } from './dialogs'
import { iconFor, ISpinner } from './Icons'

export default function InfoDialog({ path, onClose }: { path: string; onClose: () => void }) {
  const q = useQuery({ queryKey: ['info', path], queryFn: () => Api.info(path), staleTime: 0 })
  const d = q.data
  const display = '/' + path
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(display)
      toast(S.copied, 'success')
    } catch {
      /* clipboard unavailable */
    }
  }
  const Row = ({ label, children }: { label: string; children: React.ReactNode }) => (
    <div className="flex gap-3 py-1 text-sm">
      <span className="w-28 shrink-0 text-neutral-500">{label}</span>
      <span className="min-w-0 flex-1 break-all">{children}</span>
    </div>
  )
  return (
    <Modal onClose={onClose}>
      <div className="flex items-center gap-2">
        {d && iconFor(d.entry.name, d.entry.type, 24)}
        <h2 className="min-w-0 flex-1 truncate text-base font-semibold">{d?.entry.name || S.home}</h2>
      </div>
      {q.isLoading && <div className="mt-3"><ISpinner /></div>}
      {d && (
        <div className="mt-3 divide-y divide-neutral-200 dark:divide-neutral-800">
          <Row label={S.path}>
            <span className="font-mono text-xs">{display}</span>
            <button className="btn-ghost ml-2 !px-1.5 !py-0.5 text-xs" onClick={copy}>{S.copyPath}</button>
          </Row>
          <Row label={S.type}>{d.entry.type === 'dir' ? S.folder : d.entry.type === 'file' ? S.file : S.symlink}{d.entry.link && d.entry.type !== 'other' ? ` (${S.symlink})` : ''}</Row>
          {d.entry.type === 'file' && <Row label={S.size}>{formatBytes(d.entry.size)} ({d.entry.size.toLocaleString()} B)</Row>}
          {d.entry.mtime > 0 && <Row label={S.modified}>{formatDate(d.entry.mtime)}</Row>}
          {d.totals && (
            <Row label={S.contents}>
              {formatBytes(d.totals.bytes)} · {S.filesCount(d.totals.files)} · {S.dirsCount(d.totals.dirs)}
              {d.totals.partial && <span className="ml-1 text-xs text-amber-600">({S.partialCount})</span>}
            </Row>
          )}
          {d.favorite && <Row label={S.favorites}>★ {S.inFavorites}</Row>}
          {d.shares && (
            <div className="py-2 text-sm">
              <div className="mb-1 text-neutral-500">{S.sharesOfFolder}</div>
              {d.shares.length === 0 && <div className="text-xs text-neutral-400">{S.noSharesOfFolder}</div>}
              {d.shares.map((s) => (
                <div key={s.id} className="rounded bg-neutral-100 px-2 py-1.5 text-xs dark:bg-neutral-800">
                  <div className="font-medium">{s.name} <span className="font-normal text-neutral-500">· {s.createdBy}</span></div>
                  <div className="text-neutral-600 dark:text-neutral-300">
                    {S.expires} {formatRelative(s.expiresAt)} ({formatDate(s.expiresAt, true)}) · {S.shareAccessCount}: {s.accessCount} · {S.lastAccess}: {s.lastAccessAt ? formatRelative(s.lastAccessAt) : S.never}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      <div className="mt-4 flex justify-end">
        <button className="btn-primary" onClick={onClose}>{S.close}</button>
      </div>
    </Modal>
  )
}
