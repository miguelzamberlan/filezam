import { useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import { useAuth } from '../hooks'
import { S, errorMessage } from '../strings'
import { toast, Modal } from '../components/dialogs'
import { MenuButton } from '../components/Shell'
import TotpSetup, { RecoveryCodes } from '../components/TotpSetup'
import { IKey, ICheck } from '../components/Icons'

// Conta: troca de senha e verificação em duas etapas (ativar, desativar, novos códigos).
export default function Account() {
  const { user } = useAuth()
  const qc = useQueryClient()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [pwErr, setPwErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [setup, setSetup] = useState(false)
  const [ask, setAsk] = useState<'disable' | 'recovery' | null>(null)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [askErr, setAskErr] = useState<string | null>(null)
  const [newCodes, setNewCodes] = useState<string[] | null>(null)
  const fail = (e: unknown) => (e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))

  const changePassword = async (e: FormEvent) => {
    e.preventDefault()
    if (next !== confirm) return setPwErr(S.passwordMismatch)
    setBusy(true)
    setPwErr(null)
    try {
      const r = await Api.changePassword(current, next)
      qc.setQueryData(['me'], r)
      setCurrent('')
      setNext('')
      setConfirm('')
      toast(S.passwordChanged, 'success')
    } catch (e) {
      setPwErr(fail(e))
    } finally {
      setBusy(false)
    }
  }
  const submitAsk = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setAskErr(null)
    try {
      if (ask === 'disable') {
        const r = await Api.totpDisable(password, code)
        qc.setQueryData(['me'], { user: r.user })
        toast(S.totpDisabled, 'success')
        setAsk(null)
      } else {
        const r = await Api.totpRecovery(password, code)
        setNewCodes(r.recoveryCodes)
        setAsk(null)
      }
      setPassword('')
      setCode('')
    } catch (e) {
      setAskErr(fail(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-1 flex items-center gap-2"><MenuButton /><h1 className="text-lg font-semibold">{S.account}</h1></div>
      <p className="mb-4 text-sm text-neutral-500">{S.accountHint}</p>
      <div className="grid max-w-4xl gap-4 md:grid-cols-2">
        <form className="card p-4" onSubmit={changePassword}>
          <h2 className="flex items-center gap-2 font-semibold"><IKey size={16} /> {S.changePassword}</h2>
          <label className="mt-3 block text-sm">{S.currentPassword}</label>
          <input className="input mt-1" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
          <label className="mt-3 block text-sm">{S.newPassword}</label>
          <input className="input mt-1" type="password" autoComplete="new-password" minLength={8} value={next} onChange={(e) => setNext(e.target.value)} required />
          <label className="mt-3 block text-sm">{S.confirmPassword}</label>
          <input className="input mt-1" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
          {pwErr && <p className="mt-2 text-sm text-red-600">{pwErr}</p>}
          <div className="mt-4 flex justify-end"><button type="submit" className="btn-primary" disabled={busy}>{S.save}</button></div>
        </form>

        <div className="card p-4">
          <h2 className="flex items-center gap-2 font-semibold">{S.totp}
            <span className={'rounded-full px-2 py-0.5 text-xs ' + (user?.totpEnabled ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900/50 dark:text-emerald-200' : 'bg-neutral-200 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300')}>{user?.totpEnabled ? S.totpOn : S.totpOff}</span>
          </h2>
          {user?.totpRequired && <p className="mt-2 rounded bg-amber-100 px-3 py-1.5 text-sm text-amber-900 dark:bg-amber-900/40 dark:text-amber-100">{S.totpRequiredBanner}</p>}
          {!setup && !user?.totpEnabled && (
            <>
              <p className="mt-2 text-sm text-neutral-600 dark:text-neutral-400">{S.totpIntro}</p>
              <div className="mt-4 flex justify-end"><button className="btn-primary" onClick={() => setSetup(true)}>{S.totpEnable}</button></div>
            </>
          )}
          {setup && <div className="mt-3"><TotpSetup onDone={() => setSetup(false)} /></div>}
          {!setup && user?.totpEnabled && (
            <>
              <p className="mt-2 flex items-center gap-2 text-sm text-neutral-600 dark:text-neutral-400"><ICheck size={16} className="text-emerald-600" /> {S.totpIntro}</p>
              {newCodes && (
                <div className="mt-3">
                  <h3 className="text-sm font-semibold">{S.totpRecoveryTitle}</h3>
                  <p className="mt-1 text-xs text-neutral-500">{S.totpRecoveryIntro}</p>
                  <RecoveryCodes codes={newCodes} />
                </div>
              )}
              <div className="mt-4 flex flex-wrap justify-end gap-2">
                <button className="btn-ghost" onClick={() => { setAsk('recovery'); setAskErr(null) }}>{S.totpRecoveryNew}</button>
                <button className="btn-ghost text-red-600" onClick={() => { setAsk('disable'); setAskErr(null) }}>{S.totpDisable}</button>
              </div>
            </>
          )}
        </div>
      </div>
      {ask && (
        <Modal onClose={() => setAsk(null)}>
          <form onSubmit={submitAsk}>
            <h2 className="text-base font-semibold">{ask === 'disable' ? S.totpDisable : S.totpRecoveryNew}</h2>
            <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-400">{ask === 'disable' ? S.totpDisableConfirm : S.totpRecoveryNewHint}</p>
            <label className="mt-3 block text-sm">{S.password}</label>
            <input className="input mt-1" type="password" autoComplete="current-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} required />
            <label className="mt-3 block text-sm">{S.totpCodeOrRecovery}</label>
            <input className="input mt-1" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required />
            {askErr && <p className="mt-2 text-sm text-red-600">{askErr}</p>}
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" className="btn-ghost" onClick={() => setAsk(null)}>{S.cancel}</button>
              <button type="submit" className={ask === 'disable' ? 'btn-danger' : 'btn-primary'} disabled={busy}>{ask === 'disable' ? S.totpDisable : S.totpRecoveryNew}</button>
            </div>
          </form>
        </Modal>
      )}
    </div>
  )
}
