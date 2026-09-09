import { Link } from 'react-router'
import { encodePath, segments } from '../lib/paths'
import { IChevronRight, IHome } from './Icons'

export default function Breadcrumb({ path, base, rootLabel, onNavigate }: { path: string; base: string; rootLabel: string; onNavigate?: (p: string) => void }) {
  const segs = segments(path)
  const crumbs = segs.map((s, i) => ({ name: s, path: segs.slice(0, i + 1).join('/') }))
  const item = (p: string, label: React.ReactNode, last: boolean) =>
    last ? (
      <span className="truncate font-medium">{label}</span>
    ) : onNavigate ? (
      <button className="truncate rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800" onClick={() => onNavigate(p)}>{label}</button>
    ) : (
      <Link to={base + (p ? '/' + encodePath(p) : '')} className="truncate rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800">{label}</Link>
    )
  return (
    <div className="flex min-w-0 items-center gap-0.5 text-sm">
      {item('', <span className="flex items-center gap-1"><IHome size={15} /> {rootLabel}</span>, segs.length === 0)}
      {crumbs.map((c, i) => (
        <span key={c.path} className="flex min-w-0 items-center gap-0.5">
          <IChevronRight size={14} className="shrink-0 text-neutral-400" />
          {item(c.path, c.name, i === crumbs.length - 1)}
        </span>
      ))}
    </div>
  )
}
