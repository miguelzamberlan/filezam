import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import type { Job } from '../api/types'
import { formatBytes } from '../lib/format'
import { useJobs } from '../store/jobs'
import { S, errorMessage } from '../strings'
import { useInvalidateDirs } from '../hooks'
import { IClose, ISpinner, ICheck, IAlert } from './Icons'

function JobToast({ job }: { job: Job }) {
  const { update, dismiss } = useJobs()
  const invalidate = useInvalidateDirs()
  const running = job.state === 'running'
  const q = useQuery({
    queryKey: ['job', job.id],
    queryFn: () => Api.job(job.id),
    enabled: running,
    refetchInterval: running ? 500 : false,
    staleTime: 0,
  })
  useEffect(() => {
    if (q.data && q.data.job.state !== job.state) {
      update(q.data.job)
    } else if (q.data && q.data.job.state === 'running' && (q.data.job.done !== job.done || q.data.job.bytesDone !== job.bytesDone || q.data.job.current !== job.current)) {
      update(q.data.job)
    }
  }, [q.data, job, update])
  useEffect(() => {
    if (job.state !== 'running') {
      invalidate(job.dirs ?? [])
      if (job.state === 'done' && !job.warnings?.length) {
        const t = setTimeout(() => dismiss(job.id), 3000)
        return () => clearTimeout(t)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job.state])
  const label = job.type === 'copy' ? S.jobCopy : job.type === 'move' ? S.jobMove : S.jobDelete
  const pct = job.bytesTotal > 0 ? (job.bytesDone / job.bytesTotal) * 100 : job.total > 0 ? (job.done / job.total) * 100 : 0
  return (
    <div className="card w-80 p-3 text-sm">
      <div className="flex items-center gap-2">
        {job.state === 'running' ? <ISpinner size={16} /> : job.state === 'done' ? <ICheck size={16} className="text-emerald-600" /> : <IAlert size={16} className="text-red-600" />}
        <span className="font-medium">
          {label} {job.state === 'done' ? `· ${S.jobDone}` : job.state === 'failed' ? `· ${S.jobFailed}` : job.state === 'cancelled' ? `· ${S.jobCancelled}` : ''}
        </span>
        <span className="ml-auto flex gap-1">
          {job.state === 'running' && (
            <button className="btn-ghost !px-1.5 !py-0.5 text-xs" onClick={() => Api.cancelJob(job.id)}>{S.cancel}</button>
          )}
          <button className="btn-ghost !p-1" onClick={() => dismiss(job.id)} aria-label={S.close}><IClose size={14} /></button>
        </span>
      </div>
      {job.state === 'running' && (
        <>
          <div className="mt-2 h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800">
            <div className="h-full bg-accent transition-[width]" style={{ width: pct + '%' }} />
          </div>
          <div className="mt-1 truncate text-xs text-neutral-500">
            {job.done}/{job.total} {job.bytesTotal > 0 && `· ${formatBytes(job.bytesDone)} / ${formatBytes(job.bytesTotal)}`} {job.current && `· ${job.current}`}
          </div>
        </>
      )}
      {job.error && <div className="mt-1 text-xs text-red-600">{errorMessage(undefined, job.error)}</div>}
      {!!job.warnings?.length && (
        <details className="mt-1 text-xs text-amber-700 dark:text-amber-400">
          <summary>{job.warnings.length} aviso(s)</summary>
          <ul className="mt-1 max-h-32 overflow-auto">{job.warnings.map((w, i) => <li key={i} className="truncate">{w}</li>)}</ul>
        </details>
      )}
    </div>
  )
}

export default function JobToasts() {
  const jobs = useJobs((s) => s.jobs)
  if (jobs.length === 0) return null
  return (
    <div className="fixed bottom-3 left-3 z-40 flex flex-col gap-2">
      {jobs.map((j) => <JobToast key={j.id} job={j} />)}
    </div>
  )
}
