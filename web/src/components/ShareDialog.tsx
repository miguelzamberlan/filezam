import { useState } from 'react'
import { Api, ApiError } from '../api/client'
import { useConfigValue } from '../hooks'
import { copyText } from '../lib/clipboard'
import { shareLink } from '../lib/share'
import { S, errorMessage } from '../strings'
import { Modal } from './dialogs'

const OPTIONS: { label: string; seconds: number }[] = [
  { label: `1 ${S.hour}`, seconds: 3600 },
  { label: `6 ${S.hours}`, seconds: 6 * 3600 },
  { label: `24 ${S.hours}`, seconds: 86400 },
  { label: `3 ${S.days}`, seconds: 3 * 86400 },
  { label: `7 ${S.days}`, seconds: 7 * 86400 },
  { label: `30 ${S.days}`, seconds: 30 * 86400 },
]

// kind: pasta (listagem/ZIP) ou arquivo único (download/preview direto). A senha é opcional
// e vai só na criação: o servidor guarda o hash e nunca a devolve.
export default function ShareDialog({ path, name, kind = 'dir', maxTtl, onClose, onCreated }: { path: string; name: string; kind?: 'dir' | 'file'; maxTtl: number; onClose: () => void; onCreated?: () => void }) {
  const publicUrl = useConfigValue()?.publicUrl
  const opts = OPTIONS.filter((o) => o.seconds <= maxTtl)
  const [seconds, setSeconds] = useState(opts[Math.min(2, opts.length - 1)]?.seconds ?? maxTtl)
  const [password, setPassword] = useState('')
  const [url, setUrl] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const create = async () => {
    setBusy(true)
    setErr(null)
    try {
      const r = await Api.createShare(path, seconds, name, password || undefined)
      const link = shareLink(r.token, publicUrl)
      setUrl(link)
      onCreated?.()
      setCopied(await copyText(link, S.shareCreatedCopied)) // o link já vai para a área de transferência
    } catch (e) {
      setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setBusy(false)
    }
  }
  const copy = async () => {
    if (!url) return
    setCopied(await copyText(url))
  }
  return (
    <Modal onClose={onClose}>
      <h2 className="text-base font-semibold">{kind === 'file' ? S.shareFileTitle : S.shareTitle}</h2>
      <p className="mt-1 truncate text-sm text-neutral-500" title={path}>/{path}</p>
      {!url ? (
        <>
          <label className="mt-4 block text-sm">{S.shareExpires}</label>
          <select className="input mt-1" value={seconds} onChange={(e) => setSeconds(Number(e.target.value))}>
            {opts.map((o) => <option key={o.seconds} value={o.seconds}>{o.label}</option>)}
          </select>
          <label className="mt-3 block text-sm">{S.sharePassword}</label>
          <input className="input mt-1" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={4} maxLength={256} />
          <p className="mt-1 text-xs text-neutral-500">{S.sharePasswordHint}</p>
          <p className="mt-2 text-xs text-neutral-500">{S.shareReadOnly}. {kind === 'file' ? S.shareFileNote : S.shareMoveWarning}</p>
          {err && <p className="mt-2 text-sm text-red-600">{err}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button className="btn-ghost" onClick={onClose}>{S.cancel}</button>
            <button className="btn-primary" onClick={create} disabled={busy || (password.length > 0 && password.length < 4)}>{S.share}</button>
          </div>
        </>
      ) : (
        <>
          <p className="mt-3 text-sm text-emerald-700 dark:text-emerald-400">{S.shareCreated}</p>
          <label className="mt-3 block text-sm">{S.shareLink}</label>
          <input className="input mt-1 font-mono text-xs" readOnly value={url} onFocus={(e) => e.target.select()} />
          {password && <p className="mt-2 text-xs text-neutral-500">{S.shareProtected}: {S.sharePasswordRequired}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button className="btn-ghost" onClick={onClose}>{S.close}</button>
            <button className="btn-primary" onClick={copy}>{copied ? S.copied : S.copyLink}</button>
          </div>
        </>
      )}
    </Modal>
  )
}
