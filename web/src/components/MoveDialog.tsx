// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Api, ApiError } from '../api/client'
import { compareNames } from '../lib/naturalSort'
import { dirname, isWithin, join, segments } from '../lib/paths'
import { S, errorMessage } from '../strings'
import { Modal, toast } from './dialogs'
import { IArrowUp, IChevronRight, IFolder, IFolderPlus, ISpinner } from './Icons'

export interface MoveDialogProps {
  names: string[] // itens selecionados, nomes dentro de `from`
  from: string // pasta de origem, relativa ao escopo
  showHidden: boolean
  onClose: () => void
  onPick: (destDir: string) => void
}

/**
 * Seletor de pasta de destino do "Mover para…". Navega só por pastas (`GET /api/files/dirs`),
 * começando na pasta de origem — o destino mais comum é uma irmã, a um clique de "subir".
 *
 * As pastas que estão sendo movidas aparecem desabilitadas: entrar nelas só levaria a destinos
 * que o servidor recusaria (`nested`), e é melhor não deixar chegar lá do que explicar depois.
 */
export default function MoveDialog({ names, from, showHidden, onClose, onPick }: MoveDialogProps) {
  const qc = useQueryClient()
  const [dir, setDir] = useState(from)
  const [newName, setNewName] = useState<string | null>(null) // input de "nova pasta" aberto
  const [busy, setBusy] = useState(false)
  const q = useQuery<{ path: string; dirs: string[] }, ApiError>({ queryKey: ['dirs', dir], queryFn: () => Api.dirs(dir) })

  const sources = useMemo(() => names.map((n) => join(from, n)), [names, from])
  const dirs = useMemo(() => {
    const all = q.data?.dirs ?? []
    return (showHidden ? all : all.filter((d) => !d.startsWith('.'))).slice().sort(compareNames)
  }, [q.data, showHidden])

  const blocked = dir === from || sources.some((p) => isWithin(p, dir))
  const segs = segments(dir)

  const createFolder = async () => {
    const name = (newName ?? '').trim()
    if (name === '' || name.includes('/')) return toast(S.errorCodes.invalid_name, 'error')
    setBusy(true)
    try {
      const { path } = await Api.mkdir(join(dir, name))
      setNewName(null)
      qc.invalidateQueries({ queryKey: ['dirs', dir] })
      qc.invalidateQueries({ queryKey: ['list', dir] })
      setDir(path) // entra na pasta recém-criada: quem a cria aqui é para mandar os itens para dentro dela
    } catch (e) {
      toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    // Com o campo de nome aberto, Esc e o clique fora cancelam só ele: fechar o diálogo inteiro
    // faria perder também a pasta já escolhida.
    <Modal onClose={() => (newName !== null ? setNewName(null) : onClose())} size="lg">
      <h2 className="text-base font-semibold">{S.moveTitle(names.length)}</h2>
      <p className="mt-1 text-sm text-neutral-600 dark:text-neutral-400">{S.moveHint}</p>

      <div className="mt-3 flex items-center gap-1">
        <button className="btn-ghost !px-1.5" onClick={() => setDir(dirname(dir))} disabled={dir === ''} title={S.goUp}><IArrowUp size={16} /></button>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-0.5 text-xs">
          <button className="truncate rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800" onClick={() => setDir('')}>{S.home}</button>
          {segs.map((s, i) => (
            <span key={i} className="flex min-w-0 items-center gap-0.5">
              <IChevronRight size={12} className="shrink-0" />
              <button className="truncate rounded px-1 hover:bg-neutral-200 dark:hover:bg-neutral-800" onClick={() => setDir(segs.slice(0, i + 1).join('/'))}>{s}</button>
            </span>
          ))}
        </div>
      </div>

      <div className="mt-2 h-64 overflow-auto rounded-md border border-neutral-200 p-1 dark:border-neutral-800">
        {q.isLoading && <div className="flex h-full items-center justify-center text-neutral-400"><ISpinner /></div>}
        {q.error && <div className="p-2 text-sm text-red-600">{errorMessage(q.error.code, q.error.message)}</div>}
        {q.data &&
          dirs.map((d) => {
            const p = join(dir, d)
            const self = sources.includes(p)
            return (
              <button
                key={d}
                className={'flex w-full items-center gap-2 rounded px-2 py-1 text-left text-sm ' + (self ? 'cursor-not-allowed opacity-40' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800')}
                disabled={self}
                title={self ? S.errorCodes.nested : p}
                onClick={() => !self && setDir(p)}
              >
                <IFolder size={16} className="shrink-0 text-amber-500" />
                <span className="truncate">{d}</span>
              </button>
            )
          })}
        {q.data && dirs.length === 0 && <div className="flex h-full items-center justify-center text-sm text-neutral-400">{S.moveNoSubfolders}</div>}
      </div>

      {newName !== null ? (
        <form className="mt-2 flex items-center gap-2" onSubmit={(e) => (e.preventDefault(), void createFolder())}>
          <input className="input min-w-0 flex-1 !py-1 text-sm" autoFocus placeholder={S.newFolderName} value={newName} onChange={(e) => setNewName(e.target.value)} />
          <button type="button" className="btn-ghost shrink-0" onClick={() => setNewName(null)}>{S.cancel}</button>
          <button type="submit" className="btn-primary shrink-0" disabled={busy}>{S.confirm}</button>
        </form>
      ) : (
        <button className="btn-ghost mt-2 !px-1.5" onClick={() => setNewName('')}><IFolderPlus size={16} /> {S.newFolder}</button>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-neutral-200 pt-3 dark:border-neutral-800">
        <div className="min-w-0 text-sm">
          <span className="text-neutral-500">{S.moveDestination}: </span>
          <span className="break-all font-medium">{dir === '' ? S.home : '/' + dir}</span>
          {dir === from && <p className="text-xs text-neutral-500">{S.moveSameFolder}</p>}
        </div>
        <div className="flex shrink-0 gap-2">
          <button className="btn-ghost" onClick={onClose}>{S.cancel}</button>
          <button className="btn-primary" disabled={blocked} onClick={() => onPick(dir)}>{S.moveHere}</button>
        </div>
      </div>
    </Modal>
  )
}
