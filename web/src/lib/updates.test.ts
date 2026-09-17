// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { beforeEach, describe, expect, it } from 'vitest'
import { APP } from './about'
import { isOtherVersion, useUpdate } from './updates'

describe('isOtherVersion', () => {
  it('reconhece uma versão diferente da embutida no bundle', () => {
    expect(isOtherVersion(APP.version + '.1')).toBe(true)
  })
  it('ignora a própria versão e a ausência de header', () => {
    expect(isOtherVersion(APP.version)).toBe(false)
    expect(isOtherVersion(null)).toBe(false)
    expect(isOtherVersion('')).toBe(false)
  })
  // Em desenvolvimento o binário se diz 'dev' e o bundle traz o número do package.json: sem esta
  // guarda a faixa apareceria em todo `make dev`.
  it('não acusa atualização contra um servidor de desenvolvimento', () => {
    expect(isOtherVersion('dev')).toBe(false)
  })
})

describe('useUpdate', () => {
  beforeEach(() => useUpdate.setState({ serverVersion: null }))

  it('guarda a versão nova e ignora as respostas seguintes iguais a ela', () => {
    const { note } = useUpdate.getState()
    note(APP.version)
    expect(useUpdate.getState().serverVersion).toBeNull()
    note('9.9.9')
    expect(useUpdate.getState().serverVersion).toBe('9.9.9')
    note(APP.version) // a aba continua velha: o aviso não se desfaz
    expect(useUpdate.getState().serverVersion).toBe('9.9.9')
  })
})
