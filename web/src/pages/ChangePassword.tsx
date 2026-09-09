import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import { useAuth } from '../hooks'
import { S, errorMessage } from '../strings'
import { toast } from '../components/dialogs'

export default function ChangePassword() {
  const { user } = useAuth()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()
  const qc = useQueryClient()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (next !== confirm) return setErr(S.passwordMismatch)
    setBusy(true)
    setErr(null)
    try {
      const r = await Api.changePassword(current, next)
      qc.setQueryData(['me'], r)
      toast(S.passwordChanged, 'success')
      navigate('/b', { replace: true })
    } catch (e) {
      setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full items-center justify-center p-4">
      <form onSubmit={submit} className="card w-full max-w-sm p-6">
        <h1 className="text-lg font-semibold">{S.changePassword}</h1>
        {user?.mustChangePassword && <p className="mt-2 text-sm text-amber-700 dark:text-amber-400">{S.mustChangePassword}</p>}
        <label className="mt-4 block text-sm">{S.currentPassword}</label>
        <input className="input mt-1" type="password" autoComplete="current-password" autoFocus value={current} onChange={(e) => setCurrent(e.target.value)} required />
        <label className="mt-4 block text-sm">{S.newPassword}</label>
        <input className="input mt-1" type="password" autoComplete="new-password" minLength={8} value={next} onChange={(e) => setNext(e.target.value)} required />
        <label className="mt-4 block text-sm">{S.confirmPassword}</label>
        <input className="input mt-1" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
        {err && <p className="mt-3 text-sm text-red-600">{err}</p>}
        <div className="mt-6 flex gap-2">
          {!user?.mustChangePassword && <button type="button" className="btn-ghost flex-1 justify-center" onClick={() => navigate(-1)}>{S.cancel}</button>}
          <button type="submit" className="btn-primary flex-1 justify-center" disabled={busy}>{S.save}</button>
        </div>
      </form>
    </div>
  )
}
