// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Api, ApiError } from '../api/client'
import type { Entry } from '../api/types'
import { extOf, formatBytes } from '../lib/format'
import { S, errorMessage } from '../strings'
import { dialogs, toast } from './dialogs'
import { ICheck, IClose, IEye, ISpinner } from './Icons'

const Markdown = lazy(() => import('./Markdown'))
const MD = new Set(['md', 'markdown'])

// Mesmas extensões que o preview trata como texto, menos as que não fazem sentido editar à mão.
const EDITABLE = new Set([
  'txt', 'md', 'markdown', 'log', 'json', 'xml', 'yml', 'yaml', 'csv', 'ini', 'conf', 'cfg', 'toml',
  'sh', 'py', 'go', 'js', 'ts', 'tsx', 'jsx', 'css', 'html', 'htm', 'sql', 'env', 'java', 'c', 'h',
  'cpp', 'rs', 'rb', 'php', 'bat', 'ps1', 'srt', 'vtt',
])

/** editableKind: arquivo de texto dentro do limite que cabe inteiro na memória do editor. */
export function canEdit(e: Entry, maxText: number): boolean {
  return e.type === 'file' && EDITABLE.has(extOf(e.name)) && e.size <= maxText
}

export interface EditorProps {
  path: string
  entry: Entry
  maxText: number
  /** URL inline de um caminho relativo à pasta, para as imagens da prévia de markdown. */
  assetUrl?: (relPath: string) => string
  onClose: () => void
  onSaved: (e: Entry) => void
}

/**
 * Editor de texto. Vive fora do Preview de propósito: o Preview captura Escape e as setas no
 * window (para navegar entre arquivos), o que tornaria impossível digitar aqui dentro.
 */
export default function Editor({ path, entry, maxText, assetUrl, onClose, onSaved }: EditorProps) {
  const isMd = MD.has(extOf(entry.name))
  const [text, setText] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [split, setSplit] = useState(isMd)
  // mtime do que está no disco: vai em cada gravação para o servidor recusar se alguém
  // escreveu por baixo enquanto o texto estava aberto aqui.
  const [mtime, setMtime] = useState(entry.mtime)
  const original = useRef<string | null>(null)
  const dirty = text !== null && text !== original.current

  useEffect(() => {
    let cancelled = false
    fetch(Api.contentUrl(path, true), { headers: { Range: `bytes=0-${maxText - 1}` }, credentials: 'same-origin' })
      .then((r) => {
        if (!r.ok) throw new ApiError(r.status, 'internal', r.statusText)
        return r.text()
      })
      .then((t) => {
        if (cancelled) return
        original.current = t
        setText(t)
      })
      .catch((e) => !cancelled && setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e)))
    return () => {
      cancelled = true
    }
  }, [path, maxText])

  // Fechar a aba com alterações pendentes pede confirmação do navegador.
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (ev: BeforeUnloadEvent) => ev.preventDefault()
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const save = async () => {
    if (text === null || saving) return
    setSaving(true)
    setErr(null)
    try {
      const { entry: e } = await Api.putContent(path, text, mtime)
      original.current = text
      setMtime(e.mtime)
      onSaved(e)
      toast(S.saved)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'modified') setErr(S.editConflict)
      else setErr(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e))
    } finally {
      setSaving(false)
    }
  }

  const close = async () => {
    if (dirty && !(await dialogs.confirm({ title: S.unsavedChanges, message: S.discardChanges, danger: true, okLabel: S.discard }))) return
    onClose()
  }

  const onKeyDown = (ev: React.KeyboardEvent) => {
    if ((ev.ctrlKey || ev.metaKey) && ev.key === 's') {
      ev.preventDefault()
      void save()
    } else if (ev.key === 'Escape') {
      ev.preventDefault()
      void close()
    }
  }

  return (
    <div className="fixed inset-0 z-[90] flex flex-col bg-neutral-100 dark:bg-neutral-950" onKeyDown={onKeyDown}>
      <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
        <div className="min-w-0 flex-1 truncate text-sm font-medium">
          {entry.name}{dirty ? ' *' : ''}
          <span className="ml-2 text-xs font-normal text-neutral-500">{formatBytes(text?.length ?? entry.size)}</span>
        </div>
        {isMd && (
          <button className="btn-ghost" onClick={() => setSplit(!split)} title={S.previewFormatted}>
            <IEye size={16} /> <span className="hidden sm:inline">{split ? S.previewPlain : S.previewFormatted}</span>
          </button>
        )}
        <button className="btn-primary" onClick={save} disabled={!dirty || saving || text === null}>
          {saving ? <ISpinner /> : <ICheck size={16} />} {S.save}
        </button>
        <button className="btn-ghost" onClick={close} title={S.close}><IClose size={16} /></button>
      </div>
      {err && <div className="border-b border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">{err}</div>}
      <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
        {text === null ? (
          <div className="flex flex-1 items-center justify-center"><ISpinner /></div>
        ) : (
          <>
            <textarea
              className="min-h-0 flex-1 resize-none bg-white p-4 font-mono text-sm leading-relaxed outline-none dark:bg-neutral-900"
              value={text}
              onChange={(ev) => setText(ev.target.value)}
              spellCheck={false}
              autoFocus
              aria-label={entry.name}
            />
            {isMd && split && (
              <div className="min-h-0 flex-1 overflow-auto border-t border-neutral-200 bg-white px-6 py-5 dark:border-neutral-800 dark:bg-neutral-900 sm:border-l sm:border-t-0">
                <Suspense fallback={<ISpinner />}>
                  <Markdown text={text} assetUrl={assetUrl} />
                </Suspense>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
