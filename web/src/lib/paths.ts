// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

export function join(...parts: string[]): string {
  return parts.filter((p) => p !== '').join('/')
}

export function dirname(p: string): string {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(0, i) : ''
}

export function basename(p: string): string {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(i + 1) : p
}

export function segments(p: string): string[] {
  return p === '' ? [] : p.split('/')
}

/** Encode a scope-relative path for use inside the browser URL (/b/...). */
export function encodePath(p: string): string {
  return segments(p).map(encodeURIComponent).join('/')
}

export function decodePath(p: string): string {
  return p
    .split('/')
    .filter((s) => s !== '')
    .map((s) => {
      try {
        return decodeURIComponent(s)
      } catch {
        return s
      }
    })
    .join('/')
}

/**
 * Resolve an href found in a document (e.g. `./img/a%20b.png`, `../x.md#top`) against `dir`.
 * Returns a slash-joined path, or null for empty, absolute (`/…`) or hrefs that climb above `dir`.
 */
export function resolveRelative(dir: string, href: string): string | null {
  let bare = href.split(/[?#]/)[0]
  try {
    bare = decodeURIComponent(bare) // antes do split: %2F e %2E%2E contam como separador e ".."
  } catch {
    /* mantém como veio */
  }
  if (bare === '' || bare.startsWith('/')) return null
  const out = segments(dir)
  const floor = out.length
  for (const s of bare.split('/')) {
    if (s === '' || s === '.') continue
    if (s === '..') {
      if (out.length === floor) return null
      out.pop()
    } else out.push(s)
  }
  return out.length > floor ? out.join('/') : null
}

export function isWithin(parent: string, child: string): boolean {
  return parent === '' || child === parent || child.startsWith(parent + '/')
}

/**
 * nameKey é a forma de comparar dois nomes da mesma pasta para decidir conflito, espelhando
 * vfs.NameKey no servidor: sem diferenciar maiúsculas e com a mesma normalização Unicode (NFC),
 * porque Windows e Samba tratam "C8347.MP4" e "C8347.mp4" como o mesmo arquivo. Acentos contam.
 */
export function nameKey(name: string): string {
  return name.normalize('NFC').toLowerCase()
}

/** Make "name (n).ext" with nothing equivalent (see nameKey) in `taken`. */
export function uniqueName(name: string, taken: Set<string>): string {
  const keys = new Set([...taken].map(nameKey))
  if (!keys.has(nameKey(name))) return name
  const i = name.lastIndexOf('.')
  const [base, ext] = i > 0 ? [name.slice(0, i), name.slice(i)] : [name, '']
  for (let n = 1; n < 10000; n++) {
    const cand = `${base} (${n})${ext}`
    if (!keys.has(nameKey(cand))) return cand
  }
  return `${base}-${Date.now()}${ext}`
}

/** isExtractable: o Filezam extrai .zip; outros formatos continuam só para download. */
export function isExtractable(name: string): boolean {
  return name.toLowerCase().endsWith('.zip')
}

/**
 * dirChain lista a pasta e cada ancestral dela até `root` (inclusive).
 *
 * Enviar a pasta "viagem" para /fotos grava em fotos/viagem, mas quem está olhando /fotos precisa
 * ver a pasta nova aparecer: avisar só a pasta do arquivo deixa a tela aberta desatualizada.
 */
export function dirChain(dir: string, root = ''): string[] {
  const out: string[] = []
  let d = dir
  for (let i = 0; i < 64; i++) {
    out.push(d)
    if (d === root || d === '') break
    const up = dirname(d)
    if (up === d) break
    d = up
  }
  return out
}
