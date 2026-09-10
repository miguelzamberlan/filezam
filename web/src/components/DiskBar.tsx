import { useQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import { formatBytes } from '../lib/format'
import { S } from '../strings'

export default function DiskBar() {
  const q = useQuery({ queryKey: ['disk'], queryFn: () => Api.disk(), refetchInterval: 60_000, staleTime: 30_000 })
  const d = q.data
  if (!d || d.total === 0) return null
  const pct = Math.min(100, (d.used / d.total) * 100)
  return (
    <div className="px-2.5 py-2 text-xs text-neutral-500" title={`${formatBytes(d.used)} usados`}>
      <div className="mb-1 flex justify-between"><span>{S.disk}</span><span>{Math.round(pct)}%</span></div>
      <div className="h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800">
        <div className={'h-full ' + (pct > 90 ? 'bg-red-500' : pct > 75 ? 'bg-amber-500' : 'bg-accent')} style={{ width: pct + '%' }} />
      </div>
      <div className="mt-1">{S.diskFree(formatBytes(d.free), formatBytes(d.total))}</div>
    </div>
  )
}
