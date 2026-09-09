import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Api } from '../api/client'
import { formatDate, formatRelative } from '../lib/format'
import { encodePath } from '../lib/paths'
import { S } from '../strings'
import { dialogs, toast } from '../components/dialogs'
import { ITrash, ISpinner } from '../components/Icons'

export default function Shares() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['shares'], queryFn: () => Api.shares(), refetchInterval: 60_000 })
  const revoke = async (id: number, name: string) => {
    if (!(await dialogs.confirm({ title: S.shareRevoke + ': ' + name, message: S.shareRevokeConfirm, danger: true, okLabel: S.shareRevoke }))) return
    try {
      await Api.deleteShare(id)
      qc.invalidateQueries({ queryKey: ['shares'] })
    } catch {
      toast(S.error, 'error')
    }
  }
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <h1 className="mb-1 text-lg font-semibold">{S.shares}</h1>
      <p className="mb-4 text-sm text-neutral-500">{S.shareReadOnly}. {S.shareMoveWarning}</p>
      {q.isLoading && <ISpinner />}
      {q.data && q.data.shares.length === 0 && <p className="text-sm text-neutral-500">{S.shareNone}</p>}
      {q.data && q.data.shares.length > 0 && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-neutral-500">
              <tr>
                <th className="px-3 py-2">{S.name}</th>
                <th className="px-3 py-2">{S.folder}</th>
                <th className="px-3 py-2">{S.shareExpiresAt}</th>
                <th className="px-3 py-2">{S.shareAccessCount}</th>
                <th className="px-3 py-2">{S.username}</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {q.data.shares.map((s) => (
                <tr key={s.id} className="border-t border-neutral-200 dark:border-neutral-800">
                  <td className="px-3 py-2 font-medium">{s.name}</td>
                  <td className="max-w-xs truncate px-3 py-2"><Link className="text-blue-600 hover:underline" to={'/b/' + encodePath(s.path)}>/{s.path}</Link></td>
                  <td className="px-3 py-2">{s.expired ? <span className="text-red-600">{S.shareExpired}</span> : <span title={formatDate(s.expiresAt, true)}>{formatRelative(s.expiresAt)}</span>}</td>
                  <td className="px-3 py-2">{s.accessCount}{s.lastAccessAt ? <span className="text-xs text-neutral-500"> · {formatRelative(s.lastAccessAt)}</span> : ''}</td>
                  <td className="px-3 py-2">{s.createdBy}</td>
                  <td className="px-3 py-2 text-right"><button className="btn-ghost text-red-600" onClick={() => revoke(s.id, s.name)}><ITrash size={16} /> {S.shareRevoke}</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
