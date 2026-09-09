import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Api } from '../api/client'
import { useAuth, useFavorites, useInvalidateDirs } from '../hooks'
import { encodePath } from '../lib/paths'
import { S } from '../strings'
import { uploadManager } from '../upload/manager'
import { dialogs, toast } from './dialogs'
import JobToasts from './JobToasts'
import UploadPanel, { useUploads } from './UploadPanel'
import { IFolder, IStar, IShare, IUsers, ILog, ILogout, IKey, IMenu, IClose, IUpload, ISettings, ISearch } from './Icons'
import { useUI } from '../store/ui'
import DiskBar from './DiskBar'
import SettingsDialog from './SettingsDialog'

export default function Shell() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const favs = useFavorites()
  const invalidate = useInvalidateDirs()
  const uploads = useUploads()
  const [menuOpen, setMenuOpen] = useState(false)
  const [settings, setSettings] = useState(false)
  const setUploadPanelOpen = useUI((s) => s.setUploadPanelOpen)

  useEffect(() => {
    uploadManager.onConflict = (item) => dialogs.conflict(item.relPath)
    uploadManager.onDirChanged = (dir) => invalidate([dir])
    uploadManager.onError = (code) => toast(code === 'no_space' ? S.noSpace : code, 'error')
  }, [invalidate])

  const logout = async () => {
    await Api.logout().catch(() => {})
    qc.clear()
    navigate('/login', { replace: true })
  }

  const linkCls = ({ isActive }: { isActive: boolean }) =>
    'flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm ' + (isActive ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/60 dark:text-blue-100' : 'hover:bg-neutral-200 dark:hover:bg-neutral-800')

  const activeUploads = uploads.items.filter((i) => i.state === 'queued' || i.state === 'uploading').length
  // O item "Uploads" do menu é um atalho para o painel flutuante (canto inferior direito):
  // expande a lista se há envios; senão explica como enviar.
  const showUploads = () => {
    setMenuOpen(false)
    if (uploads.items.length === 0) return toast(S.uploadsEmpty, 'info')
    setUploadPanelOpen(true)
  }

  const sidebar = (
    <nav className="flex h-full w-60 shrink-0 flex-col border-r border-neutral-200 bg-white p-3 dark:border-neutral-800 dark:bg-neutral-900">
      <div className="mb-4 flex items-center gap-2 px-1">
        <img src="/favicon.svg" alt="" className="h-6 w-6" />
        <span className="text-lg font-semibold">{S.appName}</span>
        <button className="btn-ghost ml-auto !p-1 lg:hidden" onClick={() => setMenuOpen(false)}><IClose /></button>
      </div>
      <NavLink to="/b" className={linkCls} onClick={() => setMenuOpen(false)}><IFolder size={16} className="text-amber-500" /> {S.home}</NavLink>
      <NavLink to="/search" className={linkCls} onClick={() => setMenuOpen(false)}><ISearch size={16} /> {S.searchTitle}</NavLink>
      <div className="mt-3 px-2.5 text-xs font-medium uppercase tracking-wide text-neutral-500">{S.favorites}</div>
      <div className="mt-1 max-h-64 overflow-auto">
        {favs.data?.favorites.map((f) => (
          <NavLink key={f.id} to={'/b/' + encodePath(f.path)} className={linkCls} onClick={() => setMenuOpen(false)} title={f.path || '/'}
            onContextMenu={async (e) => {
              e.preventDefault()
              if (await dialogs.confirm({ title: S.removeFavorite, message: f.name })) {
                await Api.removeFavorite(f.id)
                qc.invalidateQueries({ queryKey: ['favorites'] })
              }
            }}>
            <IStar size={16} filled className="text-amber-500" /> <span className="truncate">{f.name}</span>
          </NavLink>
        ))}
        {favs.data && favs.data.favorites.length === 0 && <div className="px-2.5 py-1 text-xs text-neutral-400">—</div>}
      </div>
      <div className="mt-3 flex flex-col gap-0.5">
        <NavLink to="/shares" className={linkCls} onClick={() => setMenuOpen(false)}><IShare size={16} /> {S.shares}</NavLink>
        <button className={linkCls({ isActive: false }) + ' text-left'} onClick={showUploads} title={S.uploadsShow}>
          <IUpload size={16} /> {S.uploads}
          {activeUploads > 0 ? (
            <span className="ml-auto rounded-full bg-blue-600 px-1.5 text-xs text-white">{activeUploads}</span>
          ) : uploads.items.length > 0 ? (
            <span className="ml-auto text-xs text-neutral-500">{S.uploadsStatus(uploads.filesDone, uploads.filesTotal)}</span>
          ) : null}
        </button>
      </div>
      {user?.role === 'admin' && (
        <>
          <div className="mt-3 px-2.5 text-xs font-medium uppercase tracking-wide text-neutral-500">{S.admin}</div>
          <NavLink to="/admin/users" className={linkCls} onClick={() => setMenuOpen(false)}><IUsers size={16} /> {S.users}</NavLink>
          <NavLink to="/admin/audit" className={linkCls} onClick={() => setMenuOpen(false)}><ILog size={16} /> {S.audit}</NavLink>
        </>
      )}
      <div className="mt-auto border-t border-neutral-200 pt-2 dark:border-neutral-800">
        <DiskBar />
        <div className="truncate px-2.5 text-sm font-medium">{user?.username}</div>
        <div className="px-2.5 text-xs text-neutral-500">{user?.role === 'admin' ? S.roleAdmin : S.roleUser}{user?.restricted ? ' · ' + S.scope.toLowerCase() : ''}</div>
        <div className="mt-2 flex gap-1">
          <NavLink to="/change-password" className="btn-ghost flex-1 text-xs" title={S.changePassword}><IKey size={14} /> {S.password}</NavLink>
          <button className="btn-ghost text-xs" onClick={() => setSettings(true)} title={S.settings}><ISettings size={14} /></button>
          <button className="btn-ghost text-xs" onClick={logout} title={S.logout}><ILogout size={14} /> {S.logout}</button>
        </div>
      </div>
    </nav>
  )

  return (
    <div className="flex h-full">
      <div className="hidden lg:block">{sidebar}</div>
      {menuOpen && (
        <div className="fixed inset-0 z-50 flex lg:hidden">
          {sidebar}
          <div className="flex-1 bg-black/40" onClick={() => setMenuOpen(false)} />
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <button className="btn-ghost m-2 self-start lg:hidden" onClick={() => setMenuOpen(true)} aria-label="menu"><IMenu /></button>
        <Outlet />
      </div>
      <UploadPanel />
      <JobToasts />
      {settings && <SettingsDialog onClose={() => setSettings(false)} />}
    </div>
  )
}
