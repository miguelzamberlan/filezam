import { useInfiniteQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import { formatDate } from '../lib/format'
import { S } from '../strings'
import { ISpinner } from '../components/Icons'

export default function AdminAudit() {
  const q = useInfiniteQuery({
    queryKey: ['admin-audit'],
    queryFn: ({ pageParam }) => Api.adminAudit(pageParam as number | undefined, 100),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => (last.entries.length === 100 ? last.entries[last.entries.length - 1].id : undefined),
  })
  const entries = q.data?.pages.flatMap((p) => p.entries) ?? []
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <h1 className="mb-4 text-lg font-semibold">{S.audit}</h1>
      {q.isLoading && <ISpinner />}
      {entries.length > 0 && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-neutral-500">
              <tr>
                <th className="px-3 py-2">{S.when}</th>
                <th className="px-3 py-2">{S.username}</th>
                <th className="px-3 py-2">{S.ip}</th>
                <th className="px-3 py-2">{S.action}</th>
                <th className="px-3 py-2">{S.details}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id} className="border-t border-neutral-200 dark:border-neutral-800">
                  <td className="whitespace-nowrap px-3 py-1.5 tabular-nums">{formatDate(e.ts, true)}</td>
                  <td className="px-3 py-1.5">{e.username}</td>
                  <td className="px-3 py-1.5 font-mono text-xs">{e.ip}</td>
                  <td className={'px-3 py-1.5 font-mono text-xs ' + (e.action.includes('fail') || e.action.includes('locked') ? 'text-red-600' : '')}>{e.action}</td>
                  <td className="max-w-md truncate px-3 py-1.5 font-mono text-xs text-neutral-500" title={e.detail}>{e.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {q.hasNextPage && <button className="btn-ghost mt-3 self-center" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}>{S.loadMore}</button>}
    </div>
  )
}
