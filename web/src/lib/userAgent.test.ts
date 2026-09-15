// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { describeUserAgent } from './userAgent'

describe('describeUserAgent', () => {
  it('reconhece os navegadores e sistemas comuns', () => {
    expect(describeUserAgent('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36')).toBe('Chrome · Windows')
    expect(describeUserAgent('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0')).toBe('Edge · Windows')
    expect(describeUserAgent('Mozilla/5.0 (X11; Linux x86_64; rv:142.0) Gecko/20100101 Firefox/142.0')).toBe('Firefox · Linux')
    expect(describeUserAgent('Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36')).toBe('Chrome · Android')
    expect(describeUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1')).toBe('Safari · iOS')
    expect(describeUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Safari/605.1.15')).toBe('Safari · macOS')
    expect(describeUserAgent('curl/8.5.0')).toBe('curl')
  })

  it('sem nada reconhecível devolve o texto encurtado', () => {
    expect(describeUserAgent('')).toBe('—')
    expect(describeUserAgent('meu-script')).toBe('meu-script')
    expect(describeUserAgent('x'.repeat(60))).toBe('x'.repeat(40) + '…')
  })
})
