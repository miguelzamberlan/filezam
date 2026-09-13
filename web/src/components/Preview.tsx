// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Entry } from '../api/types'
import { extOf, formatBytes, formatDate } from '../lib/format'
import { S } from '../strings'
import { IChevronLeft, IChevronRight, IClose, IDownload, IPdf, ISpinner } from './Icons'

const IMG = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'avif', 'bmp', 'svg', 'ico'])
const VID = new Set(['mp4', 'webm', 'ogv', 'mov', 'm4v'])
const AUD = new Set(['mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac', 'opus'])
const TXT = new Set(['txt', 'md', 'markdown', 'log', 'json', 'xml', 'yml', 'yaml', 'csv', 'ini', 'conf', 'cfg', 'toml', 'sh', 'py', 'go', 'js', 'ts', 'tsx', 'jsx', 'css', 'html', 'htm', 'sql', 'env', 'java', 'c', 'h', 'cpp', 'rs', 'rb', 'php', 'bat', 'ps1', 'srt', 'vtt'])

// react-markdown + remark-gfm só são baixados quando um .md é aberto
const Markdown = lazy(() => import('./Markdown'))
const MD = new Set(['md', 'markdown'])

// Navegadores móveis não mostram PDF dentro de <iframe>: o Chrome do Android não tem visualizador embutido
// (pdfViewerEnabled = false) e o Safari do iOS/iPadOS desenha só a primeira página, sem rolagem. Nesses casos
// o preview oferece abrir em nova aba (visualizador nativo do sistema) ou baixar. iPadOS se apresenta como Mac.
const canEmbedPdf =
  navigator.pdfViewerEnabled !== false &&
  !/Android|iPhone|iPad|iPod/i.test(navigator.userAgent) &&
  !(/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1)

export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | null

// Aceita null de propósito: os chamadores decidem o rótulo do menu a partir da seleção, que pode
// não ter item único. Um crash aqui derruba a página inteira.
export function previewKind(e: Entry | null | undefined): PreviewKind {
  if (!e || e.type !== 'file') return null
  const x = extOf(e.name)
  if (IMG.has(x)) return 'image'
  if (VID.has(x)) return 'video'
  if (AUD.has(x)) return 'audio'
  if (x === 'pdf') return 'pdf'
  if (TXT.has(x)) return 'text'
  return null
}

export interface PreviewProps {
  entries: Entry[]
  index: number
  urlFor: (e: Entry, inline: boolean) => string
  maxText: number
  /** URL inline de um caminho relativo à pasta (imagens/links relativos do Markdown); omitido quando irmãos não são alcançáveis. */
  assetUrl?: (relPath: string) => string
  /** URL da miniatura já em cache, mostrada enquanto a imagem original chega; null quando não há. */
  thumbFor?: (e: Entry) => string | null
  /** Abre o arquivo no editor; ausente onde não se edita (link público). */
  onEdit?: (e: Entry) => void
  onClose: () => void
  onIndex: (i: number) => void
}

// Quantas imagens vizinhas ficam pré-carregadas de cada lado. Uma basta para as setas; mais que isso
// gasta banda do servidor com fotos que talvez ninguém abra.
const PRELOAD = 1

/**
 * Imagem do preview. Montada com `key` pela URL: reaproveitar o mesmo <img> trocando só o `src`
 * deixava a foto anterior na tela até a nova terminar de chegar, enquanto o nome no topo já tinha
 * mudado — parecia que a seta não tinha feito nada. Um elemento novo começa vazio; por baixo vai a
 * miniatura (que a listagem já baixou e o navegador guarda) com um indicador de carregamento, e a
 * original entra por cima quando termina. Vizinha já pré-carregada aparece direto, sem o fade.
 */
function ImageView({ src, thumb, alt, onClose, onReady }: { src: string; thumb: string | null; alt: string; onClose: () => void; onReady: (src: string) => void }) {
  const ref = useRef<HTMLImageElement>(null)
  const [loaded, setLoaded] = useState(false)
  const [instant, setInstant] = useState(false)
  const [thumbOk, setThumbOk] = useState(true)
  // Antes da pintura: se o navegador já tem a imagem, nem miniatura nem transição.
  useLayoutEffect(() => {
    if (ref.current?.complete && ref.current.naturalWidth > 0) {
      setInstant(true)
      setLoaded(true)
    }
  }, [])
  useEffect(() => {
    if (loaded) onReady(src)
  }, [loaded, src, onReady])
  return (
    <div className="relative flex h-full w-full items-center justify-center" onClick={(ev) => (ev.stopPropagation(), ev.target === ev.currentTarget && onClose())}>
      {!loaded && thumb && thumbOk && (
        <img src={thumb} alt="" aria-hidden onError={() => setThumbOk(false)} className="absolute inset-0 h-full w-full object-contain blur-sm" />
      )}
      {!loaded && (
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="rounded-full bg-black/50 p-3"><ISpinner size={28} /></span>
        </div>
      )}
      <img
        ref={ref}
        src={src}
        alt={alt}
        decoding="async"
        onLoad={() => setLoaded(true)}
        onError={() => setLoaded(true)}
        className={`relative max-h-full max-w-full object-contain ${instant ? '' : 'transition-opacity duration-150'} ${loaded ? 'opacity-100' : 'opacity-0'}`}
      />
    </div>
  )
}

export default function Preview({ entries, index, urlFor, maxText, assetUrl, thumbFor, onEdit, onClose, onIndex }: PreviewProps) {
  const e = entries[index]
  const kind = previewKind(e)
  const isMd = kind === 'text' && MD.has(extOf(e.name))
  const [formatted, setFormatted] = useState(true)
  const [text, setText] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const previewable = entries.map((x, i) => (previewKind(x) ? i : -1)).filter((i) => i >= 0)
  const pos = previewable.indexOf(index)
  const prev = pos > 0 ? previewable[pos - 1] : -1
  const next = pos >= 0 && pos < previewable.length - 1 ? previewable[pos + 1] : -1

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') onClose()
      else if (ev.key === 'ArrowLeft' && prev >= 0) onIndex(prev)
      else if (ev.key === 'ArrowRight' && next >= 0) onIndex(next)
      else return
      ev.stopPropagation()
      ev.preventDefault()
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [onClose, onIndex, prev, next])

  // Pré-carrega as imagens vizinhas para a seta mostrar a próxima sem espera. Só depois de a imagem
  // atual terminar: antes, as vizinhas dividiriam a banda com ela e a foto aberta demoraria mais. Sem
  // cancelar na troca: a vizinha que acabou de virar a atual é o download que o <img> aproveita.
  const [readySrc, setReadySrc] = useState<string | null>(null)
  const nearKey = [...previewable.slice(Math.max(0, pos - PRELOAD), pos), ...previewable.slice(pos + 1, pos + 1 + PRELOAD)]
    .map((i) => entries[i])
    .filter((x) => previewKind(x) === 'image')
    .map((x) => urlFor(x, true))
    .join('\n')
  const current = urlFor(e, true)
  const currentReady = kind !== 'image' || readySrc === current
  useEffect(() => {
    if (!nearKey || !currentReady) return
    for (const url of nearKey.split('\n')) {
      const img = new Image()
      img.decoding = 'async'
      img.src = url
    }
  }, [nearKey, currentReady])

  useEffect(() => {
    setText(null)
    if (kind !== 'text') return
    let cancelled = false
    setLoading(true)
    fetch(urlFor(e, true), { headers: { Range: `bytes=0-${maxText - 1}` }, credentials: 'same-origin' })
      .then((r) => r.text())
      .then((t) => !cancelled && setText(t))
      .catch(() => !cancelled && setText(S.error))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [e, kind, urlFor, maxText])

  const src = urlFor(e, true)
  const truncated = kind === 'text' && e.size > maxText
  return (
    <div className="fixed inset-0 z-[90] flex flex-col bg-black/85 text-white" onClick={onClose}>
      <div className="flex items-center gap-2 px-4 py-2" onClick={(ev) => ev.stopPropagation()}>
        <div className="min-w-0 flex-1 truncate text-sm font-medium">{e.name}</div>
        {isMd && (
          <button className="btn-ghost !text-white hover:!bg-white/20" onClick={() => setFormatted(!formatted)}>
            {formatted ? S.previewPlain : S.previewFormatted}
          </button>
        )}
        {onEdit && kind === 'text' && !truncated && (
          <button className="btn-ghost !text-white hover:!bg-white/20" onClick={() => onEdit(e)}>{S.edit}</button>
        )}
        <div className="text-xs text-neutral-300">{formatBytes(e.size)} · {formatDate(e.mtime)}</div>
        <a href={urlFor(e, false)} className="btn-ghost !text-white hover:!bg-white/20" download><IDownload size={16} /> {S.download}</a>
        <button className="btn-ghost !text-white hover:!bg-white/20" onClick={onClose}><IClose /></button>
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center p-4" onClick={(ev) => ev.target === ev.currentTarget && onClose()}>
        {prev >= 0 && (
          <button className="absolute left-2 top-1/2 z-10 -translate-y-1/2 rounded-full bg-white/10 p-2 hover:bg-white/25" onClick={(ev) => (ev.stopPropagation(), onIndex(prev))}><IChevronLeft size={24} /></button>
        )}
        {next >= 0 && (
          <button className="absolute right-2 top-1/2 z-10 -translate-y-1/2 rounded-full bg-white/10 p-2 hover:bg-white/25" onClick={(ev) => (ev.stopPropagation(), onIndex(next))}><IChevronRight size={24} /></button>
        )}
        {kind === 'image' && <ImageView key={src} src={src} thumb={thumbFor?.(e) ?? null} alt={e.name} onClose={onClose} onReady={setReadySrc} />}
        {kind === 'video' && <video src={src} controls autoPlay preload="metadata" className="max-h-full max-w-full" onClick={(ev) => ev.stopPropagation()} />}
        {kind === 'audio' && <audio src={src} controls autoPlay className="w-full max-w-lg" onClick={(ev) => ev.stopPropagation()} />}
        {/* Sem atributo sandbox: o Chrome recusa o visualizador de PDF em frames com sandbox (mesmo com allow-scripts); a resposta já vem com CSP `sandbox` do servidor, que isola o documento. */}
        {kind === 'pdf' && canEmbedPdf && <iframe src={src} title={e.name} className="h-full w-full max-w-5xl rounded bg-white" onClick={(ev) => ev.stopPropagation()} />}
        {kind === 'pdf' && !canEmbedPdf && (
          <div className="flex max-w-sm flex-col items-center gap-4 text-center" onClick={(ev) => ev.stopPropagation()}>
            <IPdf size={64} className="text-red-400" />
            <div className="text-sm text-neutral-300">{S.pdfNoInline}</div>
            <div className="flex flex-wrap justify-center gap-2">
              <a href={src} target="_blank" rel="noopener" className="btn-primary">{S.openInNewTab}</a>
              <a href={urlFor(e, false)} className="btn-ghost !text-white hover:!bg-white/20" download><IDownload size={16} /> {S.download}</a>
            </div>
          </div>
        )}
        {kind === 'text' && isMd && formatted && (
          <div className="h-full w-full max-w-4xl overflow-auto rounded bg-white px-6 py-5 text-neutral-900 dark:bg-neutral-900 dark:text-neutral-100 sm:px-10" onClick={(ev) => ev.stopPropagation()}>
            {loading || text === null ? <ISpinner /> : (
              <Suspense fallback={<ISpinner />}>
                <Markdown text={text} assetUrl={assetUrl} />
              </Suspense>
            )}
            {truncated && <div className="mt-4 text-xs text-amber-600 dark:text-amber-300">… ({formatBytes(maxText)} de {formatBytes(e.size)})</div>}
          </div>
        )}
        {kind === 'text' && !(isMd && formatted) && (
          <div className="h-full w-full max-w-5xl overflow-auto rounded bg-neutral-900 p-4" onClick={(ev) => ev.stopPropagation()}>
            {loading ? <ISpinner /> : <pre className="whitespace-pre-wrap break-words font-mono text-xs text-neutral-100">{text}</pre>}
            {truncated && <div className="mt-2 text-xs text-amber-300">… ({formatBytes(maxText)} de {formatBytes(e.size)})</div>}
          </div>
        )}
        {kind === null && <div className="text-sm text-neutral-300">{S.preview}: —</div>}
      </div>
    </div>
  )
}
