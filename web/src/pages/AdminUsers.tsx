import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { AdminUser } from '../api/types'
import { formatBytes, formatDate } from '../lib/format'
import { S, errorMessage } from '../strings'
import { Modal, dialogs, toast } from '../components/dialogs'
import { IChevronRight, IFolder, ISpinner, ITrash, IEdit, IUsers } from '../components/Icons'
import { MenuButton } from '../components/Shell'

function ScopePicker({ value, onChange, onClose }: { value: string; onChange: (v: string) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['admin-dirs', value], queryFn: () => Api.adminDirs(value) })
  const segs = value ? value.split('/') : []
  return (
    <div className="card mt-1 p-2 text-sm">
      <p className="mb-2 text-xs text-neutral-500">{S.scopeHint}</p>
      <div className="mb-1 flex flex-wrap items-center gap-0.5 text-xs">
        <button type="button" className="rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800" onClick={() => onChange('')}>{S.scopeRoot}</button>
        {segs.map((s, i) => (
          <span key={i} className="flex items-center gap-0.5">
            <IChevronRight size={12} />
            <button type="button" className="rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800" onClick={() => onChange(segs.slice(0, i + 1).join('/'))}>{s}</button>
          </span>
        ))}
      </div>
      <div className="max-h-48 overflow-auto">
        {q.isLoading && <ISpinner size={14} />}
        {q.data?.dirs.map((d) => (
          <button type="button" key={d} className="flex w-full items-center gap-2 rounded px-2 py-1 text-left hover:bg-neutral-100 dark:hover:bg-neutral-800" onClick={() => onChange(value ? value + '/' + d : d)}>
            <IFolder size={14} className="text-amber-500" /> {d}
          </button>
        ))}
        {q.data && q.data.dirs.length === 0 && <div className="px-2 text-xs text-neutral-400">—</div>}
      </div>
      <div className="mt-2 flex items-center justify-between gap-2 border-t border-neutral-200 pt-2 dark:border-neutral-800">
        <span className="truncate font-medium">{value === '' ? S.scopeRoot : '/' + value}</span>
        <button type="button" className="btn-primary !py-1" onClick={onClose}>{S.scopeUse}</button>
      </div>
    </div>
  )
}

function UserForm({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
  const qc = useQueryClient()
  const [username, setUsername] = useState(user?.username ?? '')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState(user?.role ?? 'user')
  const [scope, setScope] = useState(user?.scope ?? '')
  const [disabled, setDisabled] = useState(user?.disabled ?? false)
  const [mustChange, setMustChange] = useState(user ? user.mustChangePassword : true)
  const [quotaGb, setQuotaGb] = useState(user && user.quota > 0 ? String(Math.round((user.quota / 2 ** 30) * 100) / 100) : '')
  const quotaBytes = quotaGb.trim() === '' ? 0 : Math.round(Number(quotaGb.replace(',', '.')) * 2 ** 30)
  const [pick, setPick] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      if (user) {
        const patch: Parameters<typeof Api.adminUpdateUser>[1] = { role, scope, disabled, mustChangePassword: mustChange, quota: quotaBytes }
        if (password) patch.password = password
        await Api.adminUpdateUser(user.id, patch)
      } else {
        await Api.adminCreateUser({ username, password, role, scope, mustChangePassword: mustChange, quota: quotaBytes })
      }
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      onClose()
    } catch (e) {
      setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal onClose={onClose}>
      <form onSubmit={submit}>
        <h2 className="text-base font-semibold">{user ? S.userEdit : S.userNew}</h2>
        <label className="mt-3 block text-sm">{S.username}</label>
        <input className="input mt-1" value={username} onChange={(e) => setUsername(e.target.value)} disabled={!!user} required minLength={2} />
        <label className="mt-3 block text-sm">{user ? S.resetPassword : S.password}</label>
        <input className="input mt-1" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required={!user} minLength={8} placeholder={user ? '••••••••' : ''} />
        <label className="mt-3 block text-sm">{S.role}</label>
        <select className="input mt-1" value={role} onChange={(e) => setRole(e.target.value as 'admin' | 'user')}>
          <option value="user">{S.roleUser}</option>
          <option value="admin">{S.roleAdmin}</option>
        </select>
        <label className="mt-3 block text-sm">{S.scope}</label>
        <div className="mt-1 flex items-center gap-2">
          <span className={'input flex-1 truncate ' + (scope ? 'text-accent' : '')}>{scope === '' ? S.scopeRoot : '/' + scope}</span>
          <button type="button" className="btn-ghost" onClick={() => setPick((p) => !p)}>{S.scopePick}</button>
        </div>
        {pick && <ScopePicker value={scope} onChange={setScope} onClose={() => setPick(false)} />}
        <label className="mt-3 block text-sm">{S.quota}</label>
        <div className="mt-1 flex items-center gap-2">
          <input className="input !w-32" type="number" min={0} step={0.5} placeholder="0" value={quotaGb} onChange={(e) => setQuotaGb(e.target.value)} />
          <span className="text-sm text-neutral-500">GB</span>
        </div>
        <p className="mt-1 text-xs text-neutral-500">{S.quotaHint}</p>
        <label className="mt-3 flex items-center gap-2 text-sm"><input type="checkbox" checked={mustChange} onChange={(e) => setMustChange(e.target.checked)} /> {S.forcePasswordChange}</label>
        {user && <label className="mt-2 flex items-center gap-2 text-sm"><input type="checkbox" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} /> {S.disabled}</label>}
        {user?.totpEnabled && (
          <div className="mt-3 flex items-center justify-between rounded-md border border-neutral-200 px-3 py-2 text-sm dark:border-neutral-800">
            <span>{S.totp}: <span className="text-emerald-600">{S.totpOn}</span></span>
            <button type="button" className="btn-ghost !py-1 text-red-600" onClick={async () => {
              if (!(await dialogs.confirm({ title: S.totpResetAdmin, message: S.totpResetConfirm(user.username), danger: true, okLabel: S.totpResetAdmin }))) return
              try {
                await Api.adminTOTPReset(user.id)
                qc.invalidateQueries({ queryKey: ['admin-users'] })
                onClose()
              } catch (e) {
                setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
              }
            }}>{S.totpResetAdmin}</button>
          </div>
        )}
        {err && <p className="mt-3 text-sm text-red-600">{err}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>{S.cancel}</button>
          <button type="submit" className="btn-primary" disabled={busy}>{S.save}</button>
        </div>
      </form>
    </Modal>
  )
}

export default function AdminUsers() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['admin-users'], queryFn: () => Api.adminUsers() })
  const [edit, setEdit] = useState<AdminUser | null | 'new'>(null)
  const del = async (u: AdminUser) => {
    if (!(await dialogs.confirm({ title: S.delete, message: S.userDeleteConfirm(u.username), danger: true, okLabel: S.delete }))) return
    try {
      await Api.adminDeleteUser(u.id)
      qc.invalidateQueries({ queryKey: ['admin-users'] })
    } catch (e) {
      toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')
    }
  }
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-4 flex items-center gap-2">
        <MenuButton />
        <h1 className="text-lg font-semibold">{S.users}</h1>
        <button className="btn-primary ml-auto" onClick={() => setEdit('new')}><IUsers size={16} /> {S.userNew}</button>
      </div>
      {q.isLoading && <ISpinner />}
      {q.data && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-neutral-500">
              <tr>
                <th className="px-3 py-2">{S.username}</th>
                <th className="px-3 py-2">{S.role}</th>
                <th className="px-3 py-2">{S.scope}</th>
                <th className="hidden px-3 py-2 sm:table-cell">{S.quota}</th>
                <th className="hidden px-3 py-2 md:table-cell">{S.totpShort}</th>
                <th className="hidden px-3 py-2 md:table-cell">{S.created}</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {q.data.users.map((u) => (
                <tr key={u.id} className={'border-t border-neutral-200 dark:border-neutral-800 ' + (u.disabled ? 'opacity-50' : '')}>
                  <td className="px-3 py-2 font-medium">
                    {u.username}
                    {u.disabled && <span className="ml-2 rounded bg-neutral-200 px-1.5 text-xs dark:bg-neutral-800">{S.disabled}</span>}
                    {u.mustChangePassword && <span className="ml-2 rounded bg-amber-100 px-1.5 text-xs text-amber-800 dark:bg-amber-900/50 dark:text-amber-200">{S.changePassword}</span>}
                  </td>
                  <td className="px-3 py-2">{u.role === 'admin' ? S.roleAdmin : S.roleUser}</td>
                  <td className="max-w-xs truncate px-3 py-2">{u.scope === '' ? S.scopeRoot : '/' + u.scope}</td>
                  <td className="hidden px-3 py-2 sm:table-cell">{u.quota > 0 ? formatBytes(u.quota, 0) : '—'}</td>
                  <td className="hidden px-3 py-2 md:table-cell">{u.totpEnabled ? <span className="text-emerald-600">{S.totpOn}</span> : <span className="text-neutral-400">—</span>}</td>
                  <td className="hidden px-3 py-2 md:table-cell">{formatDate(u.createdAt, true)}</td>
                  <td className="px-3 py-2 text-right">
                    <button className="btn-ghost" onClick={() => setEdit(u)}><IEdit size={16} /></button>
                    <button className="btn-ghost text-red-600" onClick={() => del(u)}><ITrash size={16} /></button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {edit !== null && <UserForm user={edit === 'new' ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  )
}
