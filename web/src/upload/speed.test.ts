// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, it, expect } from 'vitest'
import { SpeedMeter } from './speed'

const MB = 1 << 20

describe('SpeedMeter', () => {
  it('só amostra em janelas de um segundo', () => {
    const m = new SpeedMeter(0)
    expect(m.sample(5 * MB, 500, true)).toBe(0)
    expect(m.sample(10 * MB, 1000, true)).toBe(10 * MB)
  })

  it('suaviza a velocidade em vez de seguir cada oscilação', () => {
    const m = new SpeedMeter(0)
    m.sample(10 * MB, 1000, true)
    const v = m.sample(30 * MB, 2000, true) // instantânea de 20 MB/s
    expect(v).toBeGreaterThan(10 * MB)
    expect(v).toBeLessThan(20 * MB)
  })

  it('zera depois de uma parada, em vez de deixar um resto infinitesimal ligado', () => {
    const m = new SpeedMeter(0)
    m.sample(10 * MB, 1000, true)
    let t = 1000
    for (let i = 0; i < 200; i++) m.sample(10 * MB, (t += 1000), true)
    expect(m.value).toBe(0)
  })

  it('volta a medir assim que os bytes voltam a andar', () => {
    const m = new SpeedMeter(0)
    let t = 0
    m.sample(0, (t += 1000), true)
    for (let i = 0; i < 200; i++) m.sample(0, (t += 1000), true)
    expect(m.sample(4 * MB, (t += 1000), true)).toBe(4 * MB)
  })

  it('fila encolhida (cancelar, limpar, pedaço refeito) reancora sem zerar a medição', () => {
    const m = new SpeedMeter(0)
    let t = 0
    m.sample(1000 * MB, (t += 1000), true)
    const before = m.sample(1010 * MB, (t += 1000), true)
    // O usuário cancela um arquivo de 500 MB: o total enviado cai.
    expect(m.sample(510 * MB, (t += 1000), true)).toBe(before)
    // A janela seguinte mede a partir do novo total, e não de um déficit de 500 MB.
    expect(m.sample(520 * MB, (t += 1000), true)).toBeGreaterThan(0)
  })

  it('sem nada enviando a velocidade é zero', () => {
    const m = new SpeedMeter(0)
    m.sample(10 * MB, 1000, true)
    expect(m.sample(20 * MB, 2000, false)).toBe(0)
  })
})
