import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api } from '../api/client'
import type { JobRecord } from '../api/types'
import { formatBytes, formatDate, formatDuration } from '../lib/format'
import { S, errorMessage } from '../strings'
import { toast } from '../components/dialogs'
import { ISpinner, ICheck, IAlert, IClose, IRefresh } from '../components/Icons'
import { MenuButton } from '../components/Shell'

// Operações: histórico persistido no servidor (sobrevive a reinícios) mais o progresso ao vivo
// das que ainda rodam. O polling só fica ligado enquanto houver algo em andamento.
export default function Jobs() {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: ['jobs-history'],
    queryFn: () => Api.jobHistory(200),
    refetchInterval: (query) => (query.state.data?.jobs.some((j) => j.state === 'running') ? 1000 : false),
  })
  const jobs = q.data?.jobs ?? []
  const running = jobs.filter((j) => j.state === 'running')
  const history = jobs.filter((j) => j.state !== 'running')
  const cancel = async (id: string) => {
    try {
      await Api.cancelJob(id)
      qc.invalidateQueries({ queryKey: ['jobs-history'] })
    } catch (e) {
      toast(errorMessage(undefined, String(e)), 'error')
    }
  }
  const typeLabel = (t: JobRecord['type']) => (t === 'copy' ? S.jobCopy : t === 'move' ? S.jobMove : S.jobDelete)
  const stateIcon = (j: JobRecord) =>
    j.state === 'running' ? <ISpinner size={14} /> : j.state === 'done' ? <ICheck size={14} className="text-emerald-600" /> : j.state === 'cancelled' ? <IClose size={14} className="text-neutral-400" /> : <IAlert size={14} className="text-red-600" />
  const stateLabel = (j: JobRecord) => (j.state === 'done' ? S.jobDone : j.state === 'failed' ? S.jobFailed : j.state === 'cancelled' ? S.jobCancelled : S.jobsRunning)
  const Row = ({ j }: { j: JobRecord }) => {
    const pct = j.bytesTotal > 0 ? (j.bytesDone / j.bytesTotal) * 100 : j.total > 0 ? (j.done / j.total) * 100 : 0
    const dur = j.finishedAt ? j.finishedAt - j.startedAt : Math.max(0, Math.floor(Date.now() / 1000) - j.startedAt)
    return (
      <tr className="border-t border-neutral-200 dark:border-neutral-800">
        <td className="px-3 py-1.5"><span className="flex items-center gap-1.5 whitespace-nowrap" title={j.error}>{stateIcon(j)} {stateLabel(j)}</span></td>
        <td className="px-3 py-1.5">
          <div className="font-medium">{typeLabel(j.type)}</div>
          <div className="truncate text-xs text-neutral-500" title={j.label}>{j.label}</div>
          {j.state === 'running' && (
            <div className="mt-1 h-1 w-40 overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800"><div className="h-full bg-accent" style={{ width: pct + '%' }} /></div>
          )}
          {j.error && <div className="mt-0.5 text-xs text-red-600">{j.error}</div>}
        </td>
        <td className="hidden whitespace-nowrap px-3 py-1.5 text-neutral-500 sm:table-cell">{S.jobItems(j.done, j.total)}{j.bytesTotal > 0 && ` · ${formatBytes(j.bytesDone)}`}{j.warnings > 0 && ` · ${S.jobWarnings(j.warnings)}`}</td>
        <td className="hidden whitespace-nowrap px-3 py-1.5 tabular-nums text-neutral-500 md:table-cell">{formatDate(j.startedAt, true)}</td>
        <td className="hidden whitespace-nowrap px-3 py-1.5 tabular-nums text-neutral-500 md:table-cell">{formatDuration(dur)}</td>
        <td className="px-3 py-1.5 text-right">{j.state === 'running' && <button className="btn-ghost !px-1.5 text-xs" onClick={() => cancel(j.id)}>{S.cancel}</button>}</td>
      </tr>
    )
  }
  const Table = ({ rows }: { rows: JobRecord[] }) => (
    <div className="card overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs uppercase text-neutral-500">
          <tr>
            <th className="px-3 py-2">{S.type}</th>
            <th className="px-3 py-2">{S.action}</th>
            <th className="hidden px-3 py-2 sm:table-cell">{S.contents}</th>
            <th className="hidden px-3 py-2 md:table-cell">{S.jobStarted}</th>
            <th className="hidden px-3 py-2 md:table-cell">{S.jobDuration}</th>
            <th className="px-3 py-2"></th>
          </tr>
        </thead>
        <tbody>{rows.map((j) => <Row key={j.id} j={j} />)}</tbody>
      </table>
    </div>
  )
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-1 flex items-center gap-2">
        <MenuButton />
        <h1 className="text-lg font-semibold">{S.jobsTitle}</h1>
        <button className="btn-ghost ml-auto !px-1.5" onClick={() => qc.invalidateQueries({ queryKey: ['jobs-history'] })} title={S.refresh}><IRefresh size={16} /></button>
      </div>
      <p className="mb-3 text-sm text-neutral-500">{S.jobsHint}</p>
      {q.isLoading && <ISpinner />}
      {q.data && jobs.length === 0 && <p className="text-sm text-neutral-500">{S.jobsNone}</p>}
      {running.length > 0 && (
        <>
          <h2 className="mb-2 text-sm font-semibold">{S.jobsRunning}</h2>
          <div className="mb-4"><Table rows={running} /></div>
        </>
      )}
      {history.length > 0 && (
        <>
          <h2 className="mb-2 text-sm font-semibold">{S.jobsHistory}</h2>
          <Table rows={history} />
        </>
      )}
    </div>
  )
}
