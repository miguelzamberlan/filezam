import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { SearchHit } from '../api/types'
import { formatBytes, formatDate } from '../lib/format'
import { encodePath, join, segments } from '../lib/paths'
import { useUI } from '../store/ui'
import { S, errorMessage } from '../strings'
import { iconFor, IDownload, ISearch, ISpinner, IClose } from '../components/Icons'

// Pesquisa recursiva por nome a partir de uma pasta (?path=), dentro do escopo do usuário.
// O estado fica na URL (?path=&q=) para o botão "voltar" do navegador funcionar.
export default function Search() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const showHidden = useUI((s) => s.prefs.showHidden)
  const path = params.get('path') ?? ''
  const q = params.get('q') ?? ''
  const [text, setText] = useState(q)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    setText(q)
    input.current?.focus()
  }, [q])

  const res = useQuery<{ hits: SearchHit[]; partial: boolean }, ApiError>({
    queryKey: ['search', path, q],
    queryFn: async () => {
      const r = await Api.search(path, q)
      return { hits: r.results, partial: r.partial }
    },
    enabled: q.trim().length > 0,
    staleTime: 30_000,
  })
  const hits = (res.data?.hits ?? []).filter((h) => showHidden || !(h.entry.name.startsWith('.') || segments(h.dir).some((s) => s.startsWith('.'))))

  const submit = (ev: React.FormEvent) => {
    ev.preventDefault()
    const t = text.trim()
    if (t) setParams({ path, q: t })
  }
  const folderLink = (dir: string) => '/b' + (dir ? '/' + encodePath(dir) : '')
  const hitLink = (h: SearchHit) => (h.entry.type === 'dir' ? folderLink(join(h.dir, h.entry.name)) : folderLink(h.dir) + '?sel=' + encodeURIComponent(h.entry.name))

  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <h1 className="mb-1 text-lg font-semibold">{S.searchTitle}</h1>
      <p className="mb-3 text-sm text-neutral-500">{S.searchHint}</p>
      <form className="mb-3 flex flex-wrap items-center gap-2" onSubmit={submit}>
        <input ref={input} className="input !w-80 max-w-full" placeholder={S.searchPlaceholder} value={text} onChange={(e) => setText(e.target.value)} />
        <span className="flex items-center gap-1 text-sm text-neutral-600 dark:text-neutral-300">
          {S.searchIn}
          <Link className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-xs hover:underline dark:bg-neutral-800" to={folderLink(path)} title={S.openFolder}>{path ? '/' + path : '/ (' + S.searchAllFolders + ')'}</Link>
          {path && <button type="button" className="btn-ghost !p-0.5" title={S.searchAllFolders} onClick={() => setParams({ path: '', q })}><IClose size={12} /></button>}
        </span>
        <button className="btn-primary" type="submit" disabled={!text.trim()}><ISearch size={16} /> {S.searchTitle}</button>
      </form>

      {res.isFetching && <div className="flex items-center gap-2 text-sm text-neutral-500"><ISpinner size={16} /> {S.loading}</div>}
      {res.error && <div className="text-sm text-red-600">{errorMessage(res.error.code, res.error.message)}</div>}
      {res.data && !res.isFetching && (
        <>
          <div className="mb-2 text-xs text-neutral-500">{S.searchResults(hits.length)}</div>
          {res.data.partial && <div className="mb-2 rounded bg-amber-100 px-3 py-1.5 text-sm text-amber-900 dark:bg-amber-900/40 dark:text-amber-100">{S.searchPartial}</div>}
          {hits.length > 0 && (
            <div className="card overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-left text-xs uppercase text-neutral-500">
                  <tr>
                    <th className="px-3 py-2">{S.name}</th>
                    <th className="px-3 py-2">{S.searchFolder}</th>
                    <th className="px-3 py-2 text-right">{S.size}</th>
                    <th className="hidden px-3 py-2 sm:table-cell">{S.modified}</th>
                    <th className="px-3 py-2"></th>
                  </tr>
                </thead>
                <tbody>
                  {hits.map((h) => (
                    <tr key={join(h.dir, h.entry.name)} className="border-t border-neutral-200 hover:bg-neutral-50 dark:border-neutral-800 dark:hover:bg-neutral-800/60">
                      <td className="max-w-md px-3 py-1.5">
                        <button className="flex min-w-0 max-w-full items-center gap-2 text-left hover:underline" onClick={() => navigate(hitLink(h))} title={h.entry.name}>
                          {iconFor(h.entry.name, h.entry.type)}
                          <span className="truncate">{h.entry.name}</span>
                        </button>
                      </td>
                      <td className="max-w-xs truncate px-3 py-1.5"><Link className="text-blue-600 hover:underline" to={folderLink(h.dir)} title={S.openFolder}>/{h.dir}</Link></td>
                      <td className="px-3 py-1.5 text-right tabular-nums text-neutral-500">{h.entry.type === 'dir' ? '—' : formatBytes(h.entry.size)}</td>
                      <td className="hidden px-3 py-1.5 tabular-nums text-neutral-500 sm:table-cell">{formatDate(h.entry.mtime)}</td>
                      <td className="px-3 py-1.5 text-right">
                        {h.entry.type === 'file' && <a className="btn-ghost !py-0.5" href={Api.contentUrl(join(h.dir, h.entry.name))} download title={S.download}><IDownload size={14} /></a>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}
