import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { TrashItem } from '../api/types'
import { formatBytes, formatDate } from '../lib/format'
import { encodePath, dirname } from '../lib/paths'
import { S, errorMessage } from '../strings'
import { dialogs, toast } from '../components/dialogs'
import { iconFor, ISpinner, ITrash, IRefresh } from '../components/Icons'
import { MenuButton } from '../components/Shell'

// Lixeira: itens excluídos com o caminho original; restaurar devolve ao lugar (mantendo ambos se
// já houver algo lá), "excluir de vez" e "esvaziar" apagam permanentemente.
export default function Trash() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['trash'], queryFn: () => Api.trash() })
  const [sel, setSel] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const items = q.data?.items ?? []
  const retentionDays = Math.round((q.data?.retention ?? 0) / 86400)
  const selected = useMemo(() => items.filter((i) => sel.has(i.id)), [items, sel])
  const fail = (e: unknown) => toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')
  const done = () => {
    setSel(new Set())
    qc.invalidateQueries({ queryKey: ['trash'] })
    qc.invalidateQueries({ queryKey: ['list'] })
    qc.invalidateQueries({ queryKey: ['disk'] })
  }
  const toggle = (it: TrashItem) => {
    const s = new Set(sel)
    if (s.has(it.id)) s.delete(it.id)
    else s.add(it.id)
    setSel(s)
  }
  const restore = async (list = selected) => {
    if (list.length === 0) return
    setBusy(true)
    try {
      const r = await Api.trashRestore(list.map((i) => i.id))
      if (r.restored.length) toast(S.trashRestored(r.restored.length), 'success')
      if (r.failed.length) toast(S.trashRestoreFailed(r.failed.length) + ': ' + r.failed.map((f) => errorMessage(f.code)).join(', '), 'error')
      done()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  const purge = async (list = selected) => {
    if (list.length === 0) return
    if (!(await dialogs.confirm({ title: S.trashDeleteForever, message: S.trashDeleteConfirm(list.length), danger: true, okLabel: S.trashDeleteForever }))) return
    setBusy(true)
    try {
      await Api.trashDelete(list.map((i) => i.id))
      done()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  const empty = async () => {
    if (items.length === 0) return
    if (!(await dialogs.confirm({ title: S.trashEmptyAction, message: S.trashEmptyConfirm(items.length), danger: true, okLabel: S.trashEmptyAction }))) return
    setBusy(true)
    try {
      await Api.trashEmpty()
      done()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }
  const allSelected = items.length > 0 && sel.size === items.length
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-1 flex items-center gap-2">
        <MenuButton />
        <h1 className="text-lg font-semibold">{S.trash}</h1>
        <button className="btn-ghost ml-auto !px-1.5" onClick={() => qc.invalidateQueries({ queryKey: ['trash'] })} title={S.refresh}><IRefresh size={16} /></button>
      </div>
      <p className="mb-3 text-sm text-neutral-500">{q.data && q.data.retention > 0 ? S.trashHint(retentionDays) : q.data ? S.trashDisabledHint : ''}</p>
      {q.isLoading && <ISpinner />}
      {q.data && items.length === 0 && <p className="text-sm text-neutral-500">{S.trashEmptyMsg}</p>}
      {items.length > 0 && (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-1">
            <button className="btn-primary !py-1" disabled={busy || selected.length === 0} onClick={() => restore()}>{S.trashRestore}{selected.length > 0 && ` (${selected.length})`}</button>
            <button className="btn-ghost text-red-600" disabled={busy || selected.length === 0} onClick={() => purge()}><ITrash size={16} /> {S.trashDeleteForever}</button>
            <button className="btn-ghost ml-auto text-red-600" disabled={busy} onClick={empty}>{S.trashEmptyAction}</button>
          </div>
          <div className="card overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase text-neutral-500">
                <tr>
                  <th className="w-8 px-3 py-2"><input type="checkbox" checked={allSelected} onChange={() => setSel(allSelected ? new Set() : new Set(items.map((i) => i.id)))} /></th>
                  <th className="px-3 py-2">{S.name}</th>
                  <th className="hidden px-3 py-2 sm:table-cell">{S.trashOriginal}</th>
                  <th className="px-3 py-2 text-right">{S.size}</th>
                  <th className="hidden px-3 py-2 md:table-cell">{S.trashDeletedAt}</th>
                  <th className="hidden px-3 py-2 md:table-cell">{S.trashBy}</th>
                  <th className="px-3 py-2"></th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.id} className={'border-t border-neutral-200 dark:border-neutral-800 ' + (sel.has(it.id) ? 'row-selected' : 'hover:bg-neutral-50 dark:hover:bg-neutral-800/60')} onClick={() => toggle(it)}>
                    <td className="px-3 py-1.5"><input type="checkbox" checked={sel.has(it.id)} onChange={() => toggle(it)} onClick={(e) => e.stopPropagation()} /></td>
                    <td className="max-w-md px-3 py-1.5">
                      <div className="flex items-center gap-2"><span className="shrink-0">{iconFor(it.name, it.type)}</span><span className="truncate" title={it.name}>{it.name}</span></div>
                      <Link className="block truncate text-xs text-accent sm:hidden" to={'/b/' + encodePath(dirname(it.path))} onClick={(e) => e.stopPropagation()}>/{dirname(it.path)}</Link>
                    </td>
                    <td className="hidden max-w-xs truncate px-3 py-1.5 sm:table-cell"><Link className="text-accent hover:underline" to={'/b/' + encodePath(dirname(it.path))} onClick={(e) => e.stopPropagation()} title={S.openFolder}>/{dirname(it.path)}</Link></td>
                    <td className="px-3 py-1.5 text-right tabular-nums text-neutral-500">{it.type === 'dir' ? '—' : formatBytes(it.size)}</td>
                    <td className="hidden px-3 py-1.5 tabular-nums text-neutral-500 md:table-cell">{formatDate(it.deletedAt, true)}</td>
                    <td className="hidden px-3 py-1.5 text-neutral-500 md:table-cell">{it.by}</td>
                    <td className="px-1 py-1.5 text-right sm:px-3">
                      <div className="flex justify-end gap-1 whitespace-nowrap">
                        <button className="btn-ghost !px-1.5" disabled={busy} onClick={(e) => { e.stopPropagation(); restore([it]) }}>{S.trashRestore}</button>
                        <button className="btn-ghost !px-1.5 text-red-600" disabled={busy} title={S.trashDeleteForever} onClick={(e) => { e.stopPropagation(); purge([it]) }}><ITrash size={16} /></button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}
