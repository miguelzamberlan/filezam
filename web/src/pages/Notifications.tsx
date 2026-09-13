// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { Api, ApiError } from '../api/client'
import type { AppNotification } from '../api/types'
import { formatBytes, formatDate, formatRelative } from '../lib/format'
import { encodePath } from '../lib/paths'
import { S, errorMessage } from '../strings'
import { toast } from '../components/dialogs'
import { IAlert, IBell, ICheck, IClose, IFolder, ISpinner, IUpload } from '../components/Icons'
import { MenuButton } from '../components/Shell'

// Notificações do usuário. O servidor guarda o que aconteceu; o texto é montado aqui, no idioma
// de quem lê. Abrir a pasta marca como lida; a página em si não marca nada, para uma olhada rápida
// não apagar o destaque do que ainda não foi visto.
export default function Notifications() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const q = useQuery({ queryKey: ['notifications', 'list'], queryFn: () => Api.notifications(), refetchInterval: 30_000 })
  const fail = (e: unknown) => toast(e instanceof ApiError ? errorMessage(e.code, e.message) : String(e), 'error')
  const refresh = () => qc.invalidateQueries({ queryKey: ['notifications'] })
  const read = useMutation({ mutationFn: (ids: number[] | 'all') => Api.markNotificationsRead(ids), onSuccess: refresh, onError: fail })
  const remove = useMutation({ mutationFn: (id: number) => Api.deleteNotification(id), onSuccess: refresh, onError: fail })

  const open = (n: AppNotification) => {
    if (!n.readAt) read.mutate([n.id])
    if (n.data.path !== undefined) navigate('/b/' + encodePath(n.data.path))
  }
  const list = q.data?.notifications ?? []

  return (
    <div className="flex h-full flex-col overflow-auto p-4">
      <div className="mb-4 flex max-w-3xl items-center gap-2">
        <MenuButton />
        <h1 className="text-lg font-semibold">{S.notifications}</h1>
        {(q.data?.unread ?? 0) > 0 && (
          <button className="btn-ghost ml-auto text-sm" onClick={() => read.mutate('all')}><ICheck size={16} /> {S.notificationsMarkAll}</button>
        )}
      </div>
      {q.isLoading ? (
        <div className="flex flex-1 items-center justify-center"><ISpinner /></div>
      ) : list.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-neutral-500">
          <IBell size={32} />
          <p className="max-w-sm text-center">{S.notificationsEmpty}</p>
        </div>
      ) : (
        <ul className="card max-w-3xl divide-y divide-neutral-200 dark:divide-neutral-800">
          {list.map((n) => (
            <li key={n.id} className={'flex items-start gap-3 px-4 py-3 ' + (n.readAt ? '' : 'bg-accent/5')}>
              <span className={'mt-0.5 shrink-0 ' + (n.kind === 'share.revoked' ? 'text-amber-600' : 'text-accent')}>
                {n.kind === 'share.revoked' ? <IAlert size={18} /> : <IUpload size={18} />}
              </span>
              <div className="min-w-0 flex-1">
                <div className={'text-sm ' + (n.readAt ? '' : 'font-semibold')}>{title(n)}</div>
                <div className="mt-0.5 text-xs text-neutral-500">{detail(n)}</div>
                <div className="mt-0.5 text-xs text-neutral-400" title={formatDate(n.updatedAt, true)}>{formatRelative(n.updatedAt)}{n.data.path !== undefined && <> · /{n.data.path}</>}</div>
                <div className="mt-1.5 flex flex-wrap gap-1">
                  {n.data.path !== undefined && n.kind === 'drop.received' && (
                    <button className="btn-ghost !px-2 !py-0.5 text-xs" onClick={() => open(n)}><IFolder size={14} /> {S.notificationOpenFolder}</button>
                  )}
                  {!n.readAt && (
                    <button className="btn-ghost !px-2 !py-0.5 text-xs" onClick={() => read.mutate([n.id])}><ICheck size={14} /> {S.notificationMarkRead}</button>
                  )}
                </div>
              </div>
              {!n.readAt && <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-accent" aria-hidden />}
              <button className="btn-ghost !p-1" onClick={() => remove.mutate(n.id)} title={S.notificationDelete} aria-label={S.notificationDelete}><IClose size={14} /></button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function title(n: AppNotification): string {
  const link = n.data.link || '—'
  switch (n.kind) {
    case 'drop.received':
      return S.notificationDrop(n.data.files ?? 0, link)
    case 'share.revoked':
      return S.notificationRevoked(link)
  }
  return n.kind
}

function detail(n: AppNotification): string {
  switch (n.kind) {
    case 'drop.received': {
      const names = n.data.names ?? []
      const more = (n.data.files ?? 0) - names.length
      return [formatBytes(n.data.bytes ?? 0), names.join(', ') + (more > 0 ? ' ' + S.notificationDropMore(more) : '')].filter(Boolean).join(' · ')
    }
    case 'share.revoked':
      return S.notificationRevokedDetail
  }
  return ''
}
