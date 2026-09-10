import { useQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import { formatBytes } from '../lib/format'
import { S } from '../strings'

// Com cota, a barra mostra o uso da cota do usuário; sem cota, o disco do escopo.
export default function DiskBar() {
  const q = useQuery({ queryKey: ['disk'], queryFn: () => Api.disk(), refetchInterval: 60_000, staleTime: 30_000 })
  const d = q.data
  if (!d) return null
  const quota = d.quota && d.quota > 0
  const total = quota ? d.quota! : d.total
  const used = quota ? (d.quotaUsed ?? 0) : d.used
  if (total === 0) return null
  const pct = Math.min(100, (used / total) * 100)
  return (
    <div className="px-2.5 py-2 text-xs text-neutral-500" title={`${formatBytes(used)} ${S.usedWord}`}>
      <div className="mb-1 flex justify-between"><span>{quota ? S.quota : S.disk}</span><span>{Math.round(pct)}%</span></div>
      <div className="h-1.5 w-full overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800">
        <div className={'h-full ' + (pct > 90 ? 'bg-red-500' : pct > 75 ? 'bg-amber-500' : 'bg-accent')} style={{ width: pct + '%' }} />
      </div>
      <div className="mt-1">{quota ? S.quotaUsed(formatBytes(used), formatBytes(total)) : S.diskFree(formatBytes(d.free), formatBytes(d.total))}</div>
    </div>
  )
}
