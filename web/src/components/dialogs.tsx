// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useEffect, useRef, useState, type ReactNode } from 'react'
import { create } from 'zustand'
import { S } from '../strings'
import { basename } from '../lib/paths'
import { useReloadHold } from '../lib/updates'
import type { ConflictAnswer } from '../upload/manager'
import { IAlert } from './Icons'
import UpdateBanner from './UpdateBanner'

/** Aviso destacado dentro de uma confirmação, com uma lista curta do que é afetado. */
export interface ConfirmWarning {
  title: string
  text?: string
  items?: string[]
}

interface ConfirmOpts {
  title: string
  message?: string
  danger?: boolean
  okLabel?: string
  requireCheck?: string
  warning?: ConfirmWarning
}

type Dialog =
  | { kind: 'prompt'; title: string; label?: string; initial?: string; selectExt?: boolean; validate?: (v: string) => string | null; resolve: (v: string | null) => void }
  | { kind: 'confirm'; resolve: (ok: boolean) => void } & ConfirmOpts
  | { kind: 'conflict'; name: string; existing?: string; resolve: (a: ConflictAnswer) => void }
  | { kind: 'custom'; render: (close: () => void) => ReactNode; resolve: () => void }

interface DialogState {
  stack: Dialog[]
  push: (d: Dialog) => void
  pop: () => void
}

const useDialogs = create<DialogState>((set) => ({
  stack: [],
  push: (d) => set((s) => ({ stack: [...s.stack, d] })),
  pop: () => set((s) => ({ stack: s.stack.slice(0, -1) })),
}))

export const dialogs = {
  prompt(opts: { title: string; label?: string; initial?: string; selectExt?: boolean; validate?: (v: string) => string | null }) {
    return new Promise<string | null>((resolve) => useDialogs.getState().push({ kind: 'prompt', ...opts, resolve }))
  },
  // requireCheck: texto de uma caixa que começa desmarcada e precisa ser marcada para liberar o botão (ações irreversíveis).
  confirm(opts: ConfirmOpts) {
    return new Promise<boolean>((resolve) => useDialogs.getState().push({ kind: 'confirm', ...opts, resolve }))
  },
  /** existing: o nome que já está no destino, quando difere de name só na caixa ou na forma Unicode. */
  conflict(name: string, existing?: string) {
    return new Promise<ConflictAnswer>((resolve) => useDialogs.getState().push({ kind: 'conflict', name, existing, resolve }))
  },
  custom(render: (close: () => void) => ReactNode) {
    return new Promise<void>((resolve) => useDialogs.getState().push({ kind: 'custom', render, resolve }))
  },
}

export function Modal({ children, onClose, wide, size }: { children: ReactNode; onClose?: () => void; wide?: boolean; size?: 'md' | 'lg' | 'xl' }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose?.()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [onClose])
  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-black/40 p-4" onMouseDown={(e) => e.target === e.currentTarget && onClose?.()}>
      <div className={'card max-h-full w-full overflow-y-auto p-5 ' + (wide || size === 'xl' ? 'max-w-3xl' : size === 'lg' ? 'max-w-xl' : 'max-w-md')} role="dialog" aria-modal onKeyDown={(e) => e.stopPropagation()}>
        {children}
      </div>
    </div>
  )
}

function PromptDialog({ d, close }: { d: Extract<Dialog, { kind: 'prompt' }>; close: () => void }) {
  const [value, setValue] = useState(d.initial ?? '')
  const [err, setErr] = useState<string | null>(null)
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.focus()
    if (d.selectExt) {
      const i = (d.initial ?? '').lastIndexOf('.')
      el.setSelectionRange(0, i > 0 ? i : el.value.length)
    } else el.select()
  }, [d.initial, d.selectExt])
  const submit = () => {
    const v = value.trim()
    const e = d.validate?.(v) ?? (v === '' ? S.errorCodes.invalid_name : null)
    if (e) return setErr(e)
    d.resolve(v)
    close()
  }
  return (
    <Modal onClose={() => (d.resolve(null), close())}>
      <form onSubmit={(e) => (e.preventDefault(), submit())}>
        <h2 className="mb-3 text-base font-semibold">{d.title}</h2>
        {d.label && <label className="mb-1 block text-sm text-neutral-600 dark:text-neutral-400">{d.label}</label>}
        <input ref={ref} className="input" value={value} onChange={(e) => (setValue(e.target.value), setErr(null))} />
        {err && <p className="mt-2 text-sm text-red-600">{err}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={() => (d.resolve(null), close())}>{S.cancel}</button>
          <button type="submit" className="btn-primary">{S.confirm}</button>
        </div>
      </form>
    </Modal>
  )
}

function ConfirmDialog({ d, close }: { d: Extract<Dialog, { kind: 'confirm' }>; close: () => void }) {
  const ref = useRef<HTMLButtonElement>(null)
  const [checked, setChecked] = useState(false)
  const ready = !d.requireCheck || checked
  // com caixa obrigatória o foco vai para "Cancelar": Enter não pode confirmar sem querer
  useEffect(() => ref.current?.focus(), [])
  return (
    <Modal onClose={() => (d.resolve(false), close())}>
      <div className="flex gap-3">
        {d.danger && <IAlert className="shrink-0 text-red-500" size={28} />}
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-semibold">{d.title}</h2>
          {d.message && <p className="mt-1 break-words text-sm text-neutral-600 dark:text-neutral-400">{d.message}</p>}
          {d.warning && (
            <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/50 dark:text-amber-100">
              <p className="font-medium">{d.warning.title}</p>
              {d.warning.text && <p className="mt-1">{d.warning.text}</p>}
              {!!d.warning.items?.length && (
                <ul className="mt-2 max-h-32 list-disc overflow-y-auto pl-5 text-xs">
                  {d.warning.items.map((it, i) => <li key={i} className="break-all">{it}</li>)}
                </ul>
              )}
            </div>
          )}
          {d.requireCheck && (
            <label className="mt-3 flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" className="h-4 w-4 accent-red-600" checked={checked} onChange={(e) => setChecked(e.target.checked)} autoFocus />
              <span>{d.requireCheck}</span>
            </label>
          )}
        </div>
      </div>
      <div className="mt-4 flex justify-end gap-2">
        <button ref={d.requireCheck ? ref : undefined} className="btn-ghost" onClick={() => (d.resolve(false), close())}>{S.cancel}</button>
        <button ref={d.requireCheck ? undefined : ref} className={d.danger ? 'btn-danger' : 'btn-primary'} disabled={!ready} onClick={() => (d.resolve(true), close())}>{d.okLabel ?? S.confirm}</button>
      </div>
    </Modal>
  )
}

function ConflictDialog({ d, close }: { d: Extract<Dialog, { kind: 'conflict' }>; close: () => void }) {
  const [all, setAll] = useState(false)
  const answer = (choice: ConflictAnswer['choice']) => (d.resolve({ choice, all }), close())
  return (
    <Modal onClose={() => answer('cancel')}>
      <h2 className="text-base font-semibold">{S.conflictTitle}</h2>
      <p className="mt-1 break-all text-sm text-neutral-600 dark:text-neutral-400">{d.existing && d.existing !== basename(d.name) ? S.conflictMessageAs(d.name, d.existing) : S.conflictMessage(d.name)}</p>
      <label className="mt-3 flex items-center gap-2 text-sm">
        <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> {S.applyToAll}
      </label>
      <div className="mt-4 flex flex-wrap justify-end gap-2">
        <button className="btn-ghost" onClick={() => answer('cancel')}>{S.cancel}</button>
        <button className="btn-ghost" onClick={() => answer('skip')}>{S.skip}</button>
        <button className="btn-ghost" onClick={() => answer('rename')}>{S.keepBoth}</button>
        <button className="btn-primary" onClick={() => answer('overwrite')}>{S.overwrite}</button>
      </div>
    </Modal>
  )
}

export function DialogHost() {
  const { stack, pop } = useDialogs()
  // Um diálogo aberto é uma pergunta esperando resposta: recarregar sozinho por baixo dele
  // descartaria a escolha e, num conflito de envio, o arquivo que ela decide.
  useReloadHold(stack.length > 0)
  const d = stack[stack.length - 1]
  if (!d) return null
  switch (d.kind) {
    case 'prompt':
      return <PromptDialog key={stack.length} d={d} close={pop} />
    case 'confirm':
      return <ConfirmDialog key={stack.length} d={d} close={pop} />
    case 'conflict':
      return <ConflictDialog key={stack.length} d={d} close={pop} />
    case 'custom':
      return <div key={stack.length}>{d.render(() => (d.resolve(), pop()))}</div>
  }
}

/** Lightweight toast notifications. */
interface Toast {
  id: number
  text: string
  kind: 'info' | 'error' | 'success'
}
interface ToastState {
  toasts: Toast[]
  push: (text: string, kind?: Toast['kind'], ms?: number) => void
  remove: (id: number) => void
}
let toastId = 1
export const useToasts = create<ToastState>((set) => ({
  toasts: [],
  push: (text, kind = 'info', ms) => {
    const id = toastId++
    set((s) => ({ toasts: [...s.toasts, { id, text, kind }] }))
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), ms ?? (kind === 'error' ? 7000 : 3500))
  },
  remove: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))

export const toast = (text: string, kind?: Toast['kind'], ms?: number) => useToasts.getState().push(text, kind, ms)

export function ToastHost() {
  const { toasts, remove } = useToasts()
  return (
    <div className="pointer-events-none fixed left-1/2 top-3 z-[110] flex -translate-x-1/2 flex-col gap-2">
      {/* A faixa de atualização divide esta pilha com os toasts em vez de ter um canto só dela:
          empilhadas na mesma coluna, nunca se cobrem. */}
      <UpdateBanner />
      {toasts.map((t) => (
        <div
          key={t.id}
          onClick={() => remove(t.id)}
          className={
            'pointer-events-auto max-w-[min(32rem,calc(100vw-2rem))] rounded-md px-4 py-2 text-sm shadow-lg ' +
            (t.kind === 'error' ? 'bg-red-600 text-white' : t.kind === 'success' ? 'bg-emerald-600 text-white' : 'bg-neutral-800 text-white dark:bg-neutral-200 dark:text-neutral-900')
          }
        >
          {t.text}
        </div>
      ))}
    </div>
  )
}
