import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import { S, errorMessage } from '../strings'

export default function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()
  const loc = useLocation()
  const qc = useQueryClient()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const { user } = await Api.login(username.trim(), password)
      qc.setQueryData(['me'], { user })
      navigate(user.mustChangePassword ? '/change-password' : ((loc.state as { from?: string })?.from ?? '/b'), { replace: true })
    } catch (e) {
      if (e instanceof ApiError) {
        setErr(e.code === 'rate_limited' ? S.loginRateLimited : e.status === 401 ? S.loginFailed : errorMessage(e.code, e.message))
      } else setErr(String(e))
    } finally {
      setBusy(false)
    }
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
