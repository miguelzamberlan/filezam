// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { basename, decodePath, dirChain, dirname, encodePath, join, nameKey, resolveRelative, uniqueName } from './paths'

describe('paths', () => {
  it('resolveRelative', () => {
    expect(resolveRelative('', 'img/a.png')).toBe('img/a.png')
    expect(resolveRelative('', './img/a%20b.png?raw=1#x')).toBe('img/a b.png')
    expect(resolveRelative('docs', '../README.md')).toBe(null) // não sobe acima da pasta
    expect(resolveRelative('docs', 'sub/../x.md')).toBe('docs/x.md')
    expect(resolveRelative('', '../x.md')).toBe(null)
    expect(resolveRelative('', '..%2F..%2Fetc')).toBe(null)
    expect(resolveRelative('', '/abs.png')).toBe(null)
    expect(resolveRelative('', '#titulo')).toBe(null)
    expect(resolveRelative('', '.')).toBe(null)
  })
  it('join/dirname/basename', () => {
    expect(join('', 'a', 'b')).toBe('a/b')
    expect(dirname('a/b/c')).toBe('a/b')
    expect(dirname('a')).toBe('')
    expect(basename('a/b')).toBe('b')
  })
  it('encode roundtrip', () => {
    const p = 'pasta ção/arq #1?.txt'
    expect(decodePath(encodePath(p))).toBe(p)
    expect(encodePath(p)).not.toContain('#')
  })
  it('uniqueName', () => {
    expect(uniqueName('a.txt', new Set())).toBe('a.txt')
    expect(uniqueName('a.txt', new Set(['a.txt']))).toBe('a (1).txt')
    expect(uniqueName('a.txt', new Set(['a.txt', 'a (1).txt']))).toBe('a (2).txt')
    // nomes equivalentes (outra caixa) também ocupam o lugar
    expect(uniqueName('C8347.MP4', new Set(['c8347.mp4', 'C8347 (1).mp4']))).toBe('C8347 (2).MP4')
  })

  it('nameKey', () => {
    expect(nameKey('C8347.MP4')).toBe(nameKey('c8347.mp4'))
    expect(nameKey('Ac\u0327a\u0303o.txt')).toBe(nameKey('a\u00e7\u00e3o.TXT'))
    expect(nameKey('relatório.pdf')).not.toBe(nameKey('relatorio.pdf'))
    expect(uniqueName('noext', new Set(['noext']))).toBe('noext (1)')
  })
})

// Enviar uma pasta grava os arquivos numa subpasta, mas quem está olhando a pasta de destino
// precisa ver a nova aparecer. Avisar só a pasta do arquivo deixava a tela desatualizada.
describe('dirChain', () => {
  it('lists the folder and every ancestor up to the destination', () => {
    expect(dirChain('fotos/viagem/dia1', 'fotos')).toEqual(['fotos/viagem/dia1', 'fotos/viagem', 'fotos'])
    expect(dirChain('fotos', 'fotos')).toEqual(['fotos'])
  })

  it('walks to the root when the destination is the root', () => {
    expect(dirChain('a/b/c', '')).toEqual(['a/b/c', 'a/b', 'a', ''])
    expect(dirChain('', '')).toEqual([''])
  })

  it('stops at the root even when the destination is not an ancestor', () => {
    // Não trava nem cresce sem limite se as duas pontas não se encontrarem.
    expect(dirChain('a/b', 'outro')).toEqual(['a/b', 'a', ''])
  })
})
