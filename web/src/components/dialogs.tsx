import { useEffect, useRef, useState, type ReactNode } from 'react'
import { create } from 'zustand'
import { S } from '../strings'
import type { ConflictAnswer } from '../upload/manager'
import { IAlert } from './Icons'

type Dialog =
  | { kind: 'prompt'; title: string; label?: string; initial?: string; selectExt?: boolean; validate?: (v: string) => string | null; resolve: (v: string | null) => void }
  | { kind: 'confirm'; title: string; message?: string; danger?: boolean; okLabel?: string; resolve: (ok: boolean) => void }
  | { kind: 'conflict'; name: string; resolve: (a: ConflictAnswer) => void }
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
  confirm(opts: { title: string; message?: string; danger?: boolean; okLabel?: string }) {
    return new Promise<boolean>((resolve) => useDialogs.getState().push({ kind: 'confirm', ...opts, resolve }))
  },
  conflict(name: string) {
    return new Promise<ConflictAnswer>((resolve) => useDialogs.getState().push({ kind: 'conflict', name, resolve }))
  },
  custom(render: (close: () => void) => ReactNode) {
    return new Promise<void>((resolve) => useDialogs.getState().push({ kind: 'custom', render, resolve }))
  },
}

export function Modal({ children, onClose, wide }: { children: ReactNode; onClose?: () => void; wide?: boolean }) {
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
      <div className={'card w-full p-5 ' + (wide ? 'max-w-3xl' : 'max-w-md')} role="dialog" aria-modal onKeyDown={(e) => e.stopPropagation()}>
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
  useEffect(() => ref.current?.focus(), [])
  return (
    <Modal onClose={() => (d.resolve(false), close())}>
      <div className="flex gap-3">
        {d.danger && <IAlert className="shrink-0 text-red-500" size={28} />}
        <div>
          <h2 className="text-base font-semibold">{d.title}</h2>
          {d.message && <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-400">{d.message}</p>}
        </div>
      </div>
      <div className="mt-4 flex justify-end gap-2">
        <button className="btn-ghost" onClick={() => (d.resolve(false), close())}>{S.cancel}</button>
        <button ref={ref} className={d.danger ? 'btn-danger' : 'btn-primary'} onClick={() => (d.resolve(true), close())}>{d.okLabel ?? S.confirm}</button>
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
      <p className="mt-1 break-all text-sm text-neutral-600 dark:text-neutral-400">{S.conflictMessage(d.name)}</p>
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
  push: (text: string, kind?: Toast['kind']) => void
  remove: (id: number) => void
}
let toastId = 1
export const useToasts = create<ToastState>((set) => ({
  toasts: [],
  push: (text, kind = 'info') => {
    const id = toastId++
    set((s) => ({ toasts: [...s.toasts, { id, text, kind }] }))
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), kind === 'error' ? 7000 : 3500)
  },
  remove: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))

export const toast = (text: string, kind?: Toast['kind']) => useToasts.getState().push(text, kind)

export function ToastHost() {
  const { toasts, remove } = useToasts()
  return (
    <div className="pointer-events-none fixed left-1/2 top-3 z-[110] flex -translate-x-1/2 flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          onClick={() => remove(t.id)}
          className={
            'pointer-events-auto rounded-md px-4 py-2 text-sm shadow-lg ' +
            (t.kind === 'error' ? 'bg-red-600 text-white' : t.kind === 'success' ? 'bg-emerald-600 text-white' : 'bg-neutral-800 text-white dark:bg-neutral-200 dark:text-neutral-900')
          }
        >
          {t.text}
        </div>
      ))}
    </div>
  )
}
