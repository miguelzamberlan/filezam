import { useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import { copyText } from '../lib/clipboard'
import { S, errorMessage } from '../strings'
import { ICopy } from './Icons'

// Cadastro do 2FA em três passos: QR/chave → código de confirmação → códigos de recuperação.
// O segredo só é gravado no servidor quando um código válido chega (evita ficar trancado fora).
export default function TotpSetup({ onDone }: { onDone?: () => void }) {
  const qc = useQueryClient()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [qr, setQr] = useState<string>('')
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    Api.totpSetup().then(setSetup).catch((e) => setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e)))
  }, [])
  useEffect(() => {
    // PNG em data: URL (img-src permite data:), sem injetar SVG no DOM
    if (setup) QRCode.toDataURL(setup.uri, { margin: 1, width: 200 }).then(setQr).catch(() => setQr(''))
  }, [setup])

  const confirm = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      const r = await Api.totpEnable(code)
      qc.setQueryData(['me'], { user: r.user })
      setCodes(r.recoveryCodes)
    } catch (e) {
      setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (codes) {
    return (
      <div>
        <h3 className="font-semibold">{S.totpRecoveryTitle}</h3>
        <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-400">{S.totpRecoveryIntro}</p>
        <RecoveryCodes codes={codes} />
        <div className="mt-4 flex justify-end">
          <button className="btn-primary" onClick={onDone}>{S.close}</button>
        </div>
      </div>
    )
  }
  return (
    <form onSubmit={confirm}>
      <p className="text-sm text-neutral-600 dark:text-neutral-400">{S.totpIntro}</p>
      <p className="mt-3 text-sm font-medium">{S.totpStep1}</p>
      <div className="mt-2 flex flex-wrap items-start gap-4">
        <div className="flex h-[200px] w-[200px] shrink-0 items-center justify-center rounded-md bg-white p-1">{qr && <img src={qr} alt="QR code" width={192} height={192} />}</div>
        <div className="min-w-0 text-sm">
          <div className="text-neutral-500">{S.totpKey}</div>
          <div className="mt-1 flex items-center gap-2">
            <code className="break-all rounded bg-neutral-100 px-2 py-1 text-xs dark:bg-neutral-800">{setup ? setup.secret.replace(/(.{4})/g, '$1 ').trim() : '…'}</code>
            {setup && <button type="button" className="btn-ghost !px-1.5" onClick={() => copyText(setup.secret)} title={S.copy}><ICopy size={14} /></button>}
          </div>
        </div>
      </div>
      <p className="mt-4 text-sm font-medium">{S.totpStep2}</p>
      <input className="input mt-2 !w-40 text-center font-mono text-lg tracking-widest" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9 ]*" maxLength={7} placeholder="000000" autoFocus value={code} onChange={(e) => setCode(e.target.value)} />
      {err && <p className="mt-2 text-sm text-red-600">{err}</p>}
      <div className="mt-4 flex justify-end gap-2">
        {onDone && <button type="button" className="btn-ghost" onClick={onDone}>{S.cancel}</button>}
        <button type="submit" className="btn-primary" disabled={busy || !setup || code.replace(/\s/g, '').length !== 6}>{S.totpEnable}</button>
      </div>
    </form>
  )
}

export function RecoveryCodes({ codes }: { codes: string[] }) {
  return (
    <div className="mt-3">
      <div className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border border-neutral-200 p-3 font-mono text-sm dark:border-neutral-800">
        {codes.map((c) => <span key={c}>{c}</span>)}
      </div>
      <button type="button" className="btn-ghost mt-2 !px-2 text-xs" onClick={() => copyText(codes.join('\n'))}><ICopy size={14} /> {S.totpCopyCodes}</button>
    </div>
  )
}
