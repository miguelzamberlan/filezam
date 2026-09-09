import type { SVGProps } from 'react'

type P = SVGProps<SVGSVGElement> & { size?: number }
const base = (props: P, children: React.ReactNode) => {
  const { size = 18, ...rest } = props
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden {...rest}>
      {children}
    </svg>
  )
}

export const IFolder = (p: P) => base(p, <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" fill="currentColor" fillOpacity={0.15} />)
export const IFile = (p: P) => base(p, <><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5" /></>)
export const IImage = (p: P) => base(p, <><rect x="3" y="4" width="18" height="16" rx="2" /><circle cx="9" cy="10" r="2" /><path d="m21 16-5-5-8 8" /></>)
export const IVideo = (p: P) => base(p, <><rect x="3" y="5" width="14" height="14" rx="2" /><path d="m17 10 4-2v8l-4-2z" /></>)
export const IAudio = (p: P) => base(p, <><path d="M9 18V6l10-2v12" /><circle cx="6" cy="18" r="3" /><circle cx="16" cy="16" r="3" /></>)
export const IArchive = (p: P) => base(p, <><rect x="3" y="4" width="18" height="5" rx="1" /><path d="M5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4" /></>)
export const ILink = (p: P) => base(p, <><path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1" /><path d="M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1" /></>)
export const IUpload = (p: P) => base(p, <><path d="M12 16V4m0 0-4 4m4-4 4 4" /><path d="M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" /></>)
export const IDownload = (p: P) => base(p, <><path d="M12 4v12m0 0-4-4m4 4 4-4" /><path d="M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" /></>)
export const IFolderPlus = (p: P) => base(p, <><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /><path d="M12 11v5m-2.5-2.5h5" /></>)
export const ICopy = (p: P) => base(p, <><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V5a2 2 0 0 1 2-2h10" /></>)
export const IScissors = (p: P) => base(p, <><circle cx="6" cy="6" r="3" /><circle cx="6" cy="18" r="3" /><path d="M20 4 8.1 15.9M14.5 14.5 20 20M8.1 8.1 12 12" /></>)
export const IPaste = (p: P) => base(p, <><rect x="8" y="2" width="8" height="4" rx="1" /><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" /></>)
export const IEdit = (p: P) => base(p, <><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" /></>)
export const ITrash = (p: P) => base(p, <><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14" /></>)
export const IShare = (p: P) => base(p, <><circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" /><path d="m8.6 13.5 6.8 4M15.4 6.5l-6.8 4" /></>)
export const IStar = (p: P & { filled?: boolean }) => base(p, <path d="m12 3 2.9 5.9 6.5.9-4.7 4.6 1.1 6.5L12 17.8 6.2 20.9l1.1-6.5L2.6 9.8l6.5-.9z" fill={p.filled ? 'currentColor' : 'none'} />)
export const IHome = (p: P) => base(p, <><path d="m3 11 9-8 9 8" /><path d="M5 10v10h5v-6h4v6h5V10" /></>)
export const IUsers = (p: P) => base(p, <><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0" /><circle cx="17" cy="9" r="2.5" /><path d="M16 15a5 5 0 0 1 5.5 5" /></>)
export const IList = (p: P) => base(p, <path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" />)
export const IChevronRight = (p: P) => base(p, <path d="m9 6 6 6-6 6" />)
export const IChevronLeft = (p: P) => base(p, <path d="m15 6-6 6 6 6" />)
export const IArrowUp = (p: P) => base(p, <><path d="M12 19V5m0 0-6 6m6-6 6 6" /></>)
export const IRefresh = (p: P) => base(p, <><path d="M20 12a8 8 0 1 1-2.3-5.7" /><path d="M20 4v5h-5" /></>)
export const IClose = (p: P) => base(p, <path d="M18 6 6 18M6 6l12 12" />)
export const ICheck = (p: P) => base(p, <path d="m5 12 5 5L20 7" />)
export const IAlert = (p: P) => base(p, <><path d="M12 3 2 20h20z" /><path d="M12 10v4m0 3h.01" /></>)
export const IGrid = (p: P) => base(p, <><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></>)
export const IPause = (p: P) => base(p, <path d="M8 5v14M16 5v14" />)
export const IPlay = (p: P) => base(p, <path d="M7 4v16l13-8z" />)
export const IEye = (p: P) => base(p, <><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></>)
export const ILogout = (p: P) => base(p, <><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" /><path d="m16 17 5-5-5-5M21 12H9" /></>)
export const IKey = (p: P) => base(p, <><circle cx="8" cy="15" r="4" /><path d="m11 12 9-9m-3 3 3 3m-6 0 2 2" /></>)
export const IMenu = (p: P) => base(p, <path d="M4 7h16M4 12h16M4 17h16" />)
export const ISpinner = (p: P) => base({ ...p, className: 'animate-spin ' + (p.className ?? '') }, <path d="M21 12a9 9 0 1 1-6.2-8.6" />)
export const ILog = (p: P) => base(p, <><path d="M4 4h16v16H4z" /><path d="M8 9h8M8 13h8M8 17h5" /></>)

export function iconFor(name: string, type: string, size = 18) {
  if (type === 'dir') return <IFolder size={size} className="text-amber-500" />
  if (type === 'other') return <ILink size={size} className="text-neutral-400" />
  const ext = name.slice(name.lastIndexOf('.') + 1).toLowerCase()
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg', 'ico', 'heic'].includes(ext)) return <IImage size={size} className="text-purple-500" />
  if (['mp4', 'webm', 'mkv', 'mov', 'avi', 'm4v'].includes(ext)) return <IVideo size={size} className="text-rose-500" />
  if (['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac', 'opus'].includes(ext)) return <IAudio size={size} className="text-emerald-500" />
  if (['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'zst'].includes(ext)) return <IArchive size={size} className="text-orange-500" />
  return <IFile size={size} className="text-neutral-500" />
}
