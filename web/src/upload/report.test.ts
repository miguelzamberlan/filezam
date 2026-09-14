// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import type { UploadItem } from './manager'
import { problemCsv, problemItems, problemText } from './report'
import { S } from '../strings'

type Item = Pick<UploadItem, 'destDir' | 'relPath' | 'size' | 'state' | 'error' | 'errorCode'>

const item = (o: Partial<Item>): Item => ({ destDir: 'fotos', relPath: 'a.jpg', size: 10, state: 'done', ...o })

describe('relatório de uploads não enviados', () => {
  const items = [
    item({ relPath: 'ok.jpg' }),
    item({ relPath: 'viagem/b.jpg', state: 'failed', errorCode: 'no_space' }),
    item({ destDir: '', relPath: 'c.txt', state: 'cancelled' }),
    item({ relPath: 'd.jpg', state: 'skipped' }),
    item({ relPath: 'e.jpg', state: 'queued' }),
  ]

  it('fica só com falhos e cancelados', () => {
    expect(problemItems(items).map((i) => i.relPath)).toEqual(['viagem/b.jpg', 'c.txt'])
  })

  it('texto traz o caminho completo e o motivo traduzido', () => {
    expect(problemText(items)).toBe(`fotos/viagem/b.jpg — ${S.errorCodes.no_space}\nc.txt — ${S.jobCancelled}`)
  })

  it('erro sem código usa a mensagem recebida', () => {
    expect(problemText([item({ state: 'failed', error: 'rede caiu' })])).toBe('fotos/a.jpg — rede caiu')
  })

  it('CSV começa com BOM, usa ";" e escapa nomes problemáticos', () => {
    const csv = problemCsv([item({ relPath: 'x;"y"\nz.txt', size: 1234, state: 'failed', errorCode: 'no_space' })])
    expect(csv.startsWith('﻿')).toBe(true)
    const lines = csv.slice(1).split('\r\n')
    const h = S.uploadReportColumns
    expect(lines[0]).toBe([h.path, h.size, h.state, h.reason].join(';'))
    expect(lines[1]).toBe(`"fotos/x;""y""\nz.txt";1234;${S.jobFailed};${S.errorCodes.no_space}`)
    expect(lines[2]).toBe('')
    expect(problemCsv([item({ state: 'cancelled' })]).split('\r\n')[1]).toBe(`fotos/a.jpg;10;${S.jobCancelled};`)
  })
})
