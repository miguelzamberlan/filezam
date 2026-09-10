import { useState } from 'react'
import { Link } from 'react-router'
import { encodePath, segments } from '../lib/paths'
import { IChevronRight, IFolder } from './Icons'
import { DRAG_MIME } from './FileList'

// onDropNames: aceita entradas arrastadas da listagem sobre um ancestral (mover para lá).
export default function Breadcrumb({ path, base, rootLabel, onNavigate, onDropNames }: { path: string; base: string; rootLabel: string; onNavigate?: (p: string) => void; onDropNames?: (dest: string, names: string[]) => void }) {
  const segs = segments(path)
  const crumbs = segs.map((s, i) => ({ name: s, path: segs.slice(0, i + 1).join('/') }))
  const [over, setOver] = useState<string | null>(null)
  const dropProps = (p: string) =>
    onDropNames
      ? {
          onDragOver: (ev: React.DragEvent) => {
            if (!ev.dataTransfer.types.includes(DRAG_MIME)) return
            ev.preventDefault()
            ev.dataTransfer.dropEffect = 'move'
            setOver(p)
          },
          onDragLeave: () => setOver(null),
          onDrop: (ev: React.DragEvent) => {
            if (!ev.dataTransfer.types.includes(DRAG_MIME)) return
            ev.preventDefault()
            setOver(null)
            try {
              onDropNames(p, JSON.parse(ev.dataTransfer.getData(DRAG_MIME)) as string[])
            } catch {
              /* payload inválido */
            }
          },
        }
      : {}
  const cls = (p: string) => 'truncate rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800' + (over === p ? ' ring-2 ring-accent' : '')
  const item = (p: string, label: React.ReactNode, last: boolean) =>
    last ? (
      <span className="truncate font-medium">{label}</span>
    ) : onNavigate ? (
      <button className={cls(p)} onClick={() => onNavigate(p)} {...dropProps(p)}>{label}</button>
    ) : (
      <Link to={base + (p ? '/' + encodePath(p) : '')} className={cls(p)} {...dropProps(p)}>{label}</Link>
    )
  return (
    <div className="flex min-w-0 items-center gap-0.5 text-sm">
      {item('', <span className="flex items-center gap-1"><IFolder size={15} className="text-amber-500" /> {rootLabel}</span>, segs.length === 0)}
      {crumbs.map((c, i) => (
        <span key={c.path} className="flex min-w-0 items-center gap-0.5">
          <IChevronRight size={14} className="shrink-0 text-neutral-400" />
          {item(c.path, c.name, i === crumbs.length - 1)}
        </span>
      ))}
    </div>
  )
}
