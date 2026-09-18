// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Api } from '../api/client'
import type { MediaBucket, MediaItem, MediaStats as Stats } from '../api/types'
import { formatBitrate, formatBytes, formatCaptureDate, formatClock, formatMegapixels, formatSpan } from '../lib/format'
import { S } from '../strings'
import { Modal } from './dialogs'
import { IAlert, IDownload, IImage, IInfo, ISpinner, IVideo } from './Icons'

/**
 * Análise técnica de um arquivo, de uma seleção ou de uma pasta inteira.
 *
 * O servidor devolve contagens com identificadores estáveis ("4k", "portrait", "mp24") e é aqui
 * que eles viram texto, como nas notificações. Quando há cabeçalho demais para ler dentro da
 * requisição, a resposta vem com um job: a tela acompanha o progresso e refaz o pedido no fim,
 * que aí sai inteiro do cache.
 */
export default function MediaStats({ paths, title, folder, onClose }: { paths: string[]; title: string; folder?: string; onClose: () => void }) {
  // `round` força uma nova consulta depois que o job de leitura termina.
  const [round, setRound] = useState(0)
  const q = useQuery({
    queryKey: ['media-stats', paths, round],
    queryFn: () => Api.mediaStats(paths),
    staleTime: 0,
    retry: false,
  })
  const jobId = q.data?.job?.id
  const job = useQuery({
    queryKey: ['media-job', jobId],
    queryFn: () => Api.job(jobId!),
    enabled: !!jobId,
    refetchInterval: 1000,
  })
  const state = job.data?.job.state
  useEffect(() => {
    if (state && state !== 'running') setRound((n) => n + 1)
  }, [state])

  const st = q.data?.stats
  const reading = q.isLoading || !!jobId
  return (
    <Modal onClose={onClose} size="xl">
      <div className="flex items-start gap-2">
        <h2 className="min-w-0 flex-1 text-base font-semibold">
          {S.mediaTitle}
          <span className="ml-2 font-normal text-neutral-500" title={title}>{title}</span>
        </h2>
        {folder !== undefined && st && (
          <a className="btn-ghost shrink-0 text-xs" href={Api.mediaCsvUrl(folder)} download>
            <IDownload size={14} /> {S.mediaExportCsv}
          </a>
        )}
        <button className="btn-ghost shrink-0" onClick={onClose}>{S.close}</button>
      </div>

      {reading && (
        <div className="mt-6 flex flex-col items-center gap-2 py-8 text-sm text-neutral-500">
          <ISpinner />
          <span>{S.mediaReading}</span>
          {jobId && q.data?.pending !== undefined && <span className="text-xs">{S.mediaQueued(q.data.pending)}</span>}
          {job.data && job.data.job.total > 0 && (
            <div className="w-64">
              <div className="meter"><div className="meter-fill" style={{ width: `${Math.round((job.data.job.done / job.data.job.total) * 100)}%` }} /></div>
              <div className="mt-1 text-center text-xs tabular-nums">{job.data.job.done} / {job.data.job.total}</div>
            </div>
          )}
        </div>
      )}
      {q.isError && <div className="mt-4 text-sm text-red-600">{String(q.error)}</div>}
      {st && <Report st={st} />}
    </Modal>
  )
}

function Report({ st }: { st: Stats }) {
  const visual = st.images + st.videos + st.audios
  if (visual === 0 && st.unreadable === 0) {
    return <div className="mt-6 py-8 text-center text-sm text-neutral-500">{S.mediaEmpty}</div>
  }
  return (
    <div className="mt-4 space-y-5">
      {st.partial && (
        <div className="flex gap-2 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
          <IAlert size={14} /> <span>{S.mediaPartial}</span>
        </div>
      )}

      {/* Só os quadros que têm o que dizer: numa pasta só de fotos, "duração total" seria um traço. */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {st.videos > 0 && <Tile label={S.mediaVideos} value={st.videos.toLocaleString()} sub={formatBytes(st.videoBytes)} icon={<IVideo size={14} />} />}
        {st.videoDuration > 0 && <Tile label={S.mediaTotalDuration} value={formatSpan(st.videoDuration)} />}
        {st.images > 0 && <Tile label={S.mediaImages} value={st.images.toLocaleString()} sub={formatBytes(st.imageBytes)} icon={<IImage size={14} />} />}
        {st.pixels > 0 && <Tile label={S.mediaTotalPixels} value={S.mediaMegapixels(formatMegapixels(st.pixels))} />}
        {st.audios > 0 && <Tile label={S.mediaAudios} value={st.audios.toLocaleString()} sub={st.audioDuration > 0 ? formatSpan(st.audioDuration) : formatBytes(st.audioBytes)} />}
      </div>

      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-neutral-500">
        <span>{S.mediaFiles}: <b className="text-neutral-700 dark:text-neutral-300">{st.files.toLocaleString()}</b></span>
        <span>{S.size}: <b className="text-neutral-700 dark:text-neutral-300">{formatBytes(st.bytes)}</b></span>
        {st.others > 0 && <span>{S.mediaOthers}: <b className="text-neutral-700 dark:text-neutral-300">{st.others.toLocaleString()}</b></span>}
        {st.unreadable > 0 && (
          <span title={S.mediaUnreadableHint}>
            {S.mediaUnreadable}: <b className="text-neutral-700 dark:text-neutral-300">{st.unreadable.toLocaleString()}</b> <IInfo size={11} />
          </span>
        )}
      </div>

      <div className="grid gap-5 sm:grid-cols-2">
        <Breakdown title={S.mediaVideoRes} rows={st.videoRes} label={(k) => S.mediaRes[k] ?? k} total={st.videos} duration />
        <Breakdown title={S.mediaImageMp} rows={st.imageMp} label={(k) => S.mediaMp[k] ?? k} total={st.images} />
        <Breakdown title={S.mediaImageShape} rows={st.imageShape} label={(k) => S.mediaShape[k] ?? k} total={st.images} />
        <Breakdown title={S.mediaVideoShape} rows={st.videoShape} label={(k) => S.mediaShape[k] ?? k} total={st.videos} />
        <Breakdown title={S.mediaImageRes} rows={st.imageRes} label={(k) => k} total={st.images} max={10} />
        <Breakdown title={S.mediaFormats} rows={st.formats} label={(k) => k.toUpperCase()} total={visual} bytes />
        <Breakdown title={S.mediaCodecs} rows={st.codecs} label={(k) => k} total={visual} />
        <Breakdown title={S.mediaFps} rows={st.fps} label={(k) => k + ' fps'} total={st.videos} duration />
        <Breakdown title={S.mediaCameras} rows={st.cameras} label={(k) => k} total={visual} />
      </div>

      {(st.longest || st.largest || st.sharpest || st.oldest) && (
        <div>
          <h3 className="mb-1 text-xs font-semibold tracking-wide text-neutral-500 uppercase">{S.mediaHighlights}</h3>
          <div className="divide-y divide-neutral-200 text-sm dark:divide-neutral-800">
            <Highlight label={S.mediaLongest} item={st.longest} detail={(i) => formatClock(i.durationMs ?? 0)} />
            <Highlight label={S.mediaLargest} item={st.largest} detail={(i) => formatBytes(i.bytes ?? 0)} />
            <Highlight label={S.mediaSharpest} item={st.sharpest} detail={(i) => `${i.width}×${i.height}`} />
            {!!st.oldest && (
              <div className="flex gap-3 py-1.5">
                <span className="w-28 shrink-0 text-neutral-500">{S.mediaDateRange}</span>
                <span className="min-w-0 flex-1">{formatCaptureDate(st.oldest)} — {formatCaptureDate(st.newest ?? st.oldest)}</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function Tile({ label, value, sub, icon }: { label: string; value: string; sub?: string; icon?: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-neutral-200 px-3 py-2 dark:border-neutral-800">
      <div className="flex items-center gap-1 text-xs text-neutral-500">{icon}{label}</div>
      <div className="mt-0.5 truncate text-lg font-semibold tabular-nums">{value}</div>
      {sub && <div className="truncate text-xs text-neutral-500">{sub}</div>}
    </div>
  )
}

/**
 * Um detalhamento em barras. A largura é proporcional ao maior item da lista, não ao total:
 * com uma faixa dominante as demais viram um fio e deixam de ser comparáveis entre si.
 */
function Breakdown({ title, rows, label, total, bytes, duration, max }: {
  title: string
  rows: MediaBucket[]
  label: (key: string) => string
  total: number
  bytes?: boolean
  duration?: boolean
  max?: number
}) {
  if (!rows?.length) return null
  const top = Math.max(...rows.map((r) => r.count), 1)
  const shown = max && rows.length > max ? rows.slice(0, max) : rows
  const rest = rows.length - shown.length
  return (
    <div>
      <h3 className="mb-1 text-xs font-semibold tracking-wide text-neutral-500 uppercase">{title}</h3>
      <div className="space-y-1">
        {shown.map((r) => (
          <div key={r.key} className="text-sm">
            <div className="flex items-baseline gap-2">
              <span className="min-w-0 flex-1 truncate" title={label(r.key)}>{label(r.key)}</span>
              <span className="shrink-0 tabular-nums">{r.count.toLocaleString()}</span>
              {total > 0 && <span className="w-10 shrink-0 text-right text-xs text-neutral-500 tabular-nums">{Math.round((r.count / total) * 100)}%</span>}
            </div>
            <div className="meter mt-0.5 !h-1">
              <div className="meter-fill" style={{ width: `${Math.max(2, Math.round((r.count / top) * 100))}%` }} />
            </div>
            {(bytes || duration) && (
              <div className="text-xs text-neutral-500">
                {bytes && r.bytes ? formatBytes(r.bytes) : null}
                {bytes && duration && r.bytes && r.durationMs ? ' · ' : null}
                {duration && r.durationMs ? formatSpan(r.durationMs) : null}
              </div>
            )}
          </div>
        ))}
        {rest > 0 && <div className="text-xs text-neutral-500">{S.mediaMoreRows(rest)}</div>}
      </div>
    </div>
  )
}

function Highlight({ label, item, detail }: { label: string; item?: MediaItem; detail: (i: MediaItem) => string }) {
  if (!item) return null
  return (
    <div className="flex gap-3 py-1.5">
      <span className="w-28 shrink-0 text-neutral-500">{label}</span>
      <span className="min-w-0 flex-1 truncate" title={'/' + item.path}>{item.name}</span>
      <span className="shrink-0 tabular-nums">{detail(item)}</span>
    </div>
  )
}

/** Linhas técnicas de um arquivo só, reaproveitadas pelo diálogo de propriedades. */
export function mediaRows(m: NonNullable<import('../api/types').EntryInfo['media']>): [string, string][] {
  const rows: [string, string][] = []
  const push = (k: string, v: string | undefined | null) => v && rows.push([k, v])
  push(S.mediaFormat, m.format?.toUpperCase())
  if (m.width && m.height) {
    const mp = m.kind === 'image' ? ` · ${S.mediaMegapixels(formatMegapixels(m.width * m.height))}` : ''
    const rot = m.rotation ? ` · ${S.mediaRotated(m.rotation)}` : ''
    push(S.mediaDimensions, `${m.width}×${m.height}${mp}${rot}`)
  }
  if (m.durationMs) push(S.mediaDuration, formatClock(m.durationMs))
  if (m.codec) push(S.mediaCodec, m.codec + (m.fpsMilli ? ` · ${m.fpsMilli / 1000} fps` : ''))
  if (m.audioCodec) {
    const parts = [m.audioCodec]
    if (m.channels) parts.push(S.mediaChannelsLabel(m.channels))
    if (m.sampleRate) parts.push(`${(m.sampleRate / 1000).toFixed(1)} kHz`)
    push(S.mediaAudioCodec, parts.join(' · '))
  }
  if (m.bitrate) push(S.mediaBitrate, formatBitrate(m.bitrate))
  push(S.mediaCamera, m.camera)
  push(S.mediaLens, m.lens)
  const shot: string[] = []
  if (m.iso) shot.push('ISO ' + m.iso)
  if (m.exposure) shot.push(m.exposure + ' s')
  if (m.fNumber) shot.push('f/' + m.fNumber)
  if (m.focalLength) shot.push(m.focalLength + ' mm')
  if (shot.length) push(S.mediaExposure, shot.join(' · '))
  if (m.takenAt) push(S.mediaTaken, `${formatCaptureDate(m.takenAt)} (${S.mediaTakenHint})`)
  return rows
}
