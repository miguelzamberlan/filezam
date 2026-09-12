import { useState } from 'react'
import { Api, ApiError } from '../api/client'
import { useConfigValue } from '../hooks'
import { copyText } from '../lib/clipboard'
import { formatBytes } from '../lib/format'
import { join } from '../lib/paths'
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

const GB = 1 << 30
const QUOTAS = [1, 2, 5, 10, 25, 50, 100, 250]

// kind: pasta (listagem/ZIP) ou arquivo único (download/preview direto). A senha é opcional e
// vai só na criação: o servidor guarda o hash e nunca a devolve.
//
// No modo "receber arquivos" o link passa a aceitar escrita anônima, então nada é opcional:
// pasta nova e vazia, cota e vencimento de no máximo 30 dias.
export default function ShareDialog({ path, name, kind = 'dir', maxTtl, onClose, onCreated }: { path: string; name: string; kind?: 'dir' | 'file'; maxTtl: number; onClose: () => void; onCreated?: () => void }) {
  const cfg = useConfigValue()
  const publicUrl = cfg?.publicUrl
  const canDrop = !!cfg?.dropEnabled && kind === 'dir'
  const canSlug = !!cfg?.slugsEnabled
  const [mode, setMode] = useState<'read' | 'drop'>('read')
  const dropTtl = Math.min(cfg?.dropMaxTtl ?? maxTtl, maxTtl)
  const opts = OPTIONS.filter((o) => o.seconds <= (mode === 'drop' ? dropTtl : maxTtl))
  const [seconds, setSeconds] = useState(OPTIONS[2].seconds)
  const [password, setPassword] = useState('')
  const [slug, setSlug] = useState('')
  const [folder, setFolder] = useState('')
  const [quota, setQuota] = useState(5 * GB)
  const [url, setUrl] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const maxQuota = cfg?.dropMaxQuota ?? 50 * GB
  const quotas = QUOTAS.map((n) => n * GB).filter((q) => q <= maxQuota)
  const dest = mode === 'drop' ? join(path, folder.trim()) : path
  // O apelido é adivinhável, então o segredo do link passa a ser a senha.
  const needsPassword = slug.trim().length > 0
  const ttl = Math.min(seconds, mode === 'drop' ? dropTtl : maxTtl)
  const invalid = busy || (password.length > 0 && password.length < 4) || (needsPassword && password.length < 4) || (mode === 'drop' && !folder.trim())

  const create = async () => {
    setBusy(true)
    setErr(null)
    try {
      const r = await Api.createShare({
        path: dest,
        expiresIn: ttl,
        name: mode === 'drop' ? folder.trim() : name,
        password: password || undefined,
        slug: slug.trim() || undefined,
        mode,
        quotaBytes: mode === 'drop' ? quota : undefined,
      })
      const link = shareLink(r.share.slug || r.token, publicUrl)
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
      <h2 className="text-base font-semibold">{mode === 'drop' ? S.dropTitle : kind === 'file' ? S.shareFileTitle : S.shareTitle}</h2>
      <p className="mt-1 truncate text-sm text-neutral-500" title={dest}>/{dest}</p>
      {!url ? (
        <>
          {canDrop && (
            <div className="mt-3 flex gap-1 rounded-md bg-neutral-100 p-1 dark:bg-neutral-800">
              {(['read', 'drop'] as const).map((m) => (
                <button
                  key={m}
                  className={'flex-1 rounded px-2 py-1 text-sm ' + (mode === m ? 'bg-white shadow-sm dark:bg-neutral-900' : 'text-neutral-500')}
                  onClick={() => setMode(m)}
                >
                  {m === 'read' ? S.shareModeRead : S.shareModeDrop}
                </button>
              ))}
            </div>
          )}
          {mode === 'drop' && (
            <>
              <label className="mt-4 block text-sm" htmlFor="drop-folder">{S.dropFolder}</label>
              <input id="drop-folder" className="input mt-1" value={folder} onChange={(e) => setFolder(e.target.value)} placeholder={S.dropTitle} autoFocus />
              <p className="mt-1 text-xs text-neutral-500">{S.dropFolderHint}</p>
              <label className="mt-3 block text-sm" htmlFor="drop-quota">{S.dropQuota}</label>
              <select id="drop-quota" className="input mt-1" value={quota} onChange={(e) => setQuota(Number(e.target.value))}>
                {quotas.map((q) => <option key={q} value={q}>{formatBytes(q)}</option>)}
              </select>
            </>
          )}
          <label className="mt-3 block text-sm" htmlFor="share-expires">{S.shareExpires}</label>
          <select id="share-expires" className="input mt-1" value={ttl} onChange={(e) => setSeconds(Number(e.target.value))}>
            {opts.map((o) => <option key={o.seconds} value={o.seconds}>{o.label}</option>)}
          </select>
          {mode === 'drop' && <p className="mt-1 text-xs text-neutral-500">{S.dropExpiryNote}</p>}
          {canSlug && (
            <>
              <label className="mt-3 block text-sm" htmlFor="share-slug">{S.shareSlug}</label>
              <input
                id="share-slug"
                className="input mt-1 font-mono text-xs"
                value={slug}
                onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                maxLength={63}
                placeholder="ex: orcamento-2026"
              />
              <p className="mt-1 text-xs text-neutral-500">{needsPassword ? S.shareSlugNeedsPassword : S.shareSlugHint}</p>
            </>
          )}
          <label className="mt-3 block text-sm" htmlFor="share-password">{S.sharePassword}</label>
          <input id="share-password" className="input mt-1" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={4} maxLength={256} />
          <p className="mt-1 text-xs text-neutral-500">{S.sharePasswordHint}</p>
          <p className="mt-2 text-xs text-neutral-500">
            {mode === 'drop' ? S.dropWriteOnly : `${S.shareReadOnly}. ${kind === 'file' ? S.shareFileNote : S.shareMoveWarning}`}
          </p>
          {err && <p className="mt-2 text-sm text-red-600">{err}</p>}
          <div className="mt-4 flex justify-end gap-2">
            <button className="btn-ghost" onClick={onClose}>{S.cancel}</button>
            <button className="btn-primary" onClick={create} disabled={invalid}>{mode === 'drop' ? S.shareModeDrop : S.share}</button>
          </div>
        </>
      ) : (
        <>
          <p className="mt-3 text-sm text-emerald-700 dark:text-emerald-400">{S.shareCreated}</p>
          <label className="mt-3 block text-sm" htmlFor="share-url">{S.shareLink}</label>
          <input id="share-url" className="input mt-1 font-mono text-xs" readOnly value={url} onFocus={(e) => e.target.select()} />
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
