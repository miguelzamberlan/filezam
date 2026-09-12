import { useState } from 'react'
import type { Entry } from '../api/types'
import { extOf } from '../lib/format'
import { iconFor } from './Icons'

// Formatos com decodificador no servidor. HEIC, AVIF, RAW e vídeo exigiriam CGO na imagem
// distroless, então continuam com o ícone por tipo.
const THUMBABLE = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'tif', 'tiff'])

export function canThumb(e: Entry): boolean {
  return e.type === 'file' && THUMBABLE.has(extOf(e.name))
}

/**
 * Miniatura de uma imagem, com o ícone por tipo como base e como rede de proteção.
 *
 * O ícone é renderizado sempre e a miniatura entra por cima quando carrega: assim a listagem não
 * "pula" enquanto as imagens chegam, e qualquer falha (formato sem decodificador, imagem grande
 * demais, recurso desligado) simplesmente deixa o ícone à mostra. `loading="lazy"` mais a
 * virtualização do FileList garantem que só o que está na tela é pedido.
 *
 * A URL carrega o mtime para que o endereço mude quando o arquivo muda — é o que permite ao
 * servidor mandar guardar a resposta sem revalidar.
 */
export default function Thumb({ entry, path, size }: { entry: Entry; path: string; size: number }) {
  const [ok, setOk] = useState(true)
  const show = ok && canThumb(entry)
  return (
    <span className="relative inline-flex items-center justify-center" style={{ width: size, height: size }}>
      {iconFor(entry.name, entry.type, size)}
      {show && (
        <img
          src={`/api/files/thumb?path=${encodeURIComponent(path)}&v=${entry.mtime}`}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setOk(false)}
          className="absolute inset-0 h-full w-full rounded-sm object-cover"
        />
      )}
    </span>
  )
}
