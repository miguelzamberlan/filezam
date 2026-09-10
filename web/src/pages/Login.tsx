import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import type { User } from '../api/types'
import { S, errorMessage } from '../strings'

export default function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [totp, setTotp] = useState<string | null>(null) // token da segunda etapa
  const [code, setCode] = useState('')
  const [trust, setTrust] = useState(false)
  const navigate = useNavigate()
  const loc = useLocation()
  const qc = useQueryClient()

  const enter = (user: User) => {
    qc.setQueryData(['me'], { user })
    navigate(user.mustChangePassword ? '/change-password' : user.totpRequired ? '/setup-2fa' : ((loc.state as { from?: string })?.from ?? '/b'), { replace: true })
  }
  const submitTotp = async (e: FormEvent) => {
    e.preventDefault()
    if (!totp) return
    setBusy(true)
    setErr(null)
    try {
      const { user } = await Api.loginTOTP(totp, code.trim(), trust)
      enter(user)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'totp_expired') {
        setTotp(null)
        setCode('')
      }
      setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setBusy(false)
    }
  }
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const r = await Api.login(username.trim(), password)
      if (r.totpRequired && r.token) {
        setTotp(r.token)
        return
      }
      if (r.user) enter(r.user)
    } catch (e) {
      if (e instanceof ApiError) {
        setErr(e.code === 'rate_limited' ? S.loginRateLimited : e.status === 401 ? S.loginFailed : errorMessage(e.code, e.message))
      } else setErr(String(e))
    } finally {
      setBusy(false)
    }
  }

  if (totp) {
    return (
      <div className="flex h-full items-center justify-center p-4">
        <form onSubmit={submitTotp} className="card w-full max-w-sm p-6">
          <div className="mb-4 flex items-center gap-2">
            <img src="/favicon.svg" alt="" className="h-8 w-8" />
            <h1 className="text-xl font-semibold">{S.totpLoginTitle}</h1>
          </div>
          <p className="text-sm text-neutral-600 dark:text-neutral-400">{S.totpLoginHint}</p>
          <input className="input mt-3 text-center font-mono text-lg tracking-widest" autoFocus autoComplete="one-time-code" inputMode="text" value={code} onChange={(e) => setCode(e.target.value)} required />
          <label className="mt-3 flex cursor-pointer items-center gap-2 text-sm"><input type="checkbox" checked={trust} onChange={(e) => setTrust(e.target.checked)} /> {S.totpTrust}</label>
          {err && <p className="mt-3 text-sm text-red-600">{err}</p>}
          <button type="submit" className="btn-primary mt-5 w-full justify-center" disabled={busy || !code.trim()}>{S.totpVerify}</button>
          <button type="button" className="btn-ghost mt-2 w-full justify-center" onClick={() => { setTotp(null); setCode(''); setErr(null) }}>{S.totpBack}</button>
        </form>
      </div>
    )
  }
  return (
    <div className="flex h-full items-center justify-center p-4">
      <form onSubmit={submit} className="card w-full max-w-sm p-6">
        <div className="mb-6 flex items-center gap-2">
          <img src="/favicon.svg" alt="" className="h-8 w-8" />
          <h1 className="text-xl font-semibold">{S.appName}</h1>
        </div>
        <label className="block text-sm">{S.username}</label>
        <input className="input mt-1" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <label className="mt-4 block text-sm">{S.password}</label>
        <input className="input mt-1" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        {err && <p className="mt-3 text-sm text-red-600">{err}</p>}
        <button type="submit" className="btn-primary mt-6 w-full justify-center" disabled={busy}>{S.login}</button>
      </form>
    </div>
  )
}
