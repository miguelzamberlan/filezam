import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Api } from '../api/client'
import { useConfigValue } from '../hooks'
import { copyText } from '../lib/clipboard'
import { formatDate, formatRelative } from '../lib/format'
import { encodePath } from '../lib/paths'
import { shareLink } from '../lib/share'
import { S } from '../strings'
import { dialogs, toast } from '../components/dialogs'
import { ICopy, ITrash, ISpinner } from '../components/Icons'
import { MenuButton } from '../components/Shell'

export default function Shares() {
  const qc = useQueryClient()
  const publicUrl = useConfigValue()?.publicUrl
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
      <div className="mb-1 flex items-center gap-2"><MenuButton /><h1 className="text-lg font-semibold">{S.shares}</h1></div>
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
                <th className="hidden px-3 py-2 sm:table-cell">{S.shareAccessCount}</th>
                <th className="hidden px-3 py-2 sm:table-cell">{S.username}</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {q.data.shares.map((s) => (
                <tr key={s.id} className="border-t border-neutral-200 dark:border-neutral-800">
                  <td className="px-3 py-2 font-medium">{s.name}</td>
                  <td className="max-w-xs truncate px-3 py-2"><Link className="text-accent hover:underline" to={'/b/' + encodePath(s.path)}>/{s.path}</Link></td>
                  <td className="px-3 py-2">{s.expired ? <span className="text-red-600">{S.shareExpired}</span> : <span title={formatDate(s.expiresAt, true)}>{formatRelative(s.expiresAt)}</span>}</td>
                  <td className="hidden px-3 py-2 sm:table-cell">{s.accessCount}{s.lastAccessAt ? <span className="text-xs text-neutral-500"> · {formatRelative(s.lastAccessAt)}</span> : ''}</td>
                  <td className="hidden px-3 py-2 sm:table-cell">{s.createdBy}</td>
                  <td className="px-1 py-2 sm:px-3">
                    <div className="flex justify-end gap-1 whitespace-nowrap">
                      {s.token ? (
                        <button className="btn-ghost !px-1.5" title={S.copyLink + ': ' + shareLink(s.token, publicUrl)} onClick={() => copyText(shareLink(s.token, publicUrl))} disabled={s.expired}><ICopy size={16} /> <span className="hidden sm:inline">{S.copyLink}</span></button>
                      ) : (
                        <span className="px-2 py-1 text-xs text-neutral-400" title={S.shareLinkGone}>—</span>
                      )}
                      <button className="btn-ghost !px-1.5 text-red-600" onClick={() => revoke(s.id, s.name)} title={S.shareRevoke}><ITrash size={16} /> <span className="hidden sm:inline">{S.shareRevoke}</span></button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
