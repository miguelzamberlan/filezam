// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, it, expect } from 'vitest'
import { formatBitrate, formatBytes, formatCaptureDate, formatClock, formatDuration, formatMegapixels, formatSpan, formatSpeed, formatRelative, typeLabel } from './format'

describe('formatSpeed', () => {
  it('sempre usa duas casas decimais, só a unidade muda', () => {
    expect(formatSpeed(0)).toBe('0.00 B/s')
    expect(formatSpeed(847.3921834781)).toBe('847.39 B/s')
    expect(formatSpeed(1024)).toBe('1.00 KB/s')
    expect(formatSpeed(1536 * 1024)).toBe('1.50 MB/s')
    expect(formatSpeed(3.25 * 1024 ** 3)).toBe('3.25 GB/s')
  })

  it('trata valores inválidos', () => {
    expect(formatSpeed(NaN)).toBe('—')
    expect(formatSpeed(-1)).toBe('—')
  })
})

describe('formatBytes', () => {
  it('não imprime fração de byte', () => {
    expect(formatBytes(847.39)).toBe('847 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
  })
})

describe('formatRelative', () => {
  it('não arredonda menos de um minuto para "0 min"', () => {
    const now = Math.floor(Date.now() / 1000)
    expect(formatRelative(now - 20)).toBe('há instantes')
    expect(formatRelative(now + 20)).toBe('em instantes')
    expect(formatRelative(now - 5 * 60)).toBe('há 5 min')
  })
})

describe('typeLabel', () => {
  it('extensão em maiúsculas; pasta e arquivo sem extensão ganham palavra', () => {
    expect(typeLabel('nota.TXT', 'file')).toBe('TXT')
    expect(typeLabel('Balanço.xlsx', 'file')).toBe('XLSX')
    expect(typeLabel('fotos', 'dir')).toBe('Pasta')
    expect(typeLabel('LEIAME', 'file')).toBe('Arquivo')
    expect(typeLabel('.bashrc', 'file')).toBe('Arquivo') // ponto inicial não é extensão
    expect(typeLabel('link', 'other')).toBe('—')
  })
})

describe('duração de mídia', () => {
  it('formatClock mostra o relógio que um reprodutor mostraria', () => {
    expect(formatClock(0)).toBe('—')
    expect(formatClock(5_000)).toBe('0:05')
    expect(formatClock(125_000)).toBe('2:05')
    expect(formatClock(3_600_000)).toBe('1:00:00')
    expect(formatClock(5_757_408)).toBe('1:35:57')
  })

  it('formatSpan resume a soma de uma pasta', () => {
    expect(formatSpan(0)).toBe('—')
    expect(formatSpan(45_000)).toBe('45 s')
    expect(formatSpan(125_000)).toBe('2 min')
    expect(formatSpan(3 * 3_600_000 + 25 * 60_000)).toBe('3 h 25 min')
  })
})

describe('formatBitrate', () => {
  it('usa múltiplos decimais, como os fabricantes anunciam', () => {
    expect(formatBitrate(0)).toBe('—')
    expect(formatBitrate(800)).toBe('800 b/s')
    expect(formatBitrate(128_000)).toBe('128 kb/s')
    expect(formatBitrate(8_922_055)).toBe('8.9 Mb/s')
    expect(formatBitrate(2_500_000_000)).toBe('2.5 Gb/s')
  })
})

describe('formatMegapixels', () => {
  it('perde a casa decimal quando o número já é grande', () => {
    expect(formatMegapixels(0)).toBe('—')
    expect(formatMegapixels(1920 * 1080)).toBe('2.1')
    expect(formatMegapixels(8160 * 6120)).toBe('49.9')
    expect(formatMegapixels(1e9)).toBe('1000')
  })
})

describe('formatCaptureDate', () => {
  it('mostra o relógio da câmera, sem deslocar pelo fuso de quem olha', () => {
    // 2026-03-14 09:41:07 lido como UTC pelo servidor: é esse relógio que tem de aparecer.
    // A comparação é contra a mesma formatação com o fuso fixado, e não contra um texto
    // escrito à mão: o formato de data e hora muda com o idioma da máquina (pt-BR escreve
    // "09:41", en-US escreve "9:41 AM"), e o que está sendo testado é o fuso, não o idioma.
    const ms = Date.UTC(2026, 2, 14, 9, 41, 7)
    const em = (tz: string) =>
      new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short', timeZone: tz }).format(new Date(ms))
    expect(formatCaptureDate(ms)).toBe(em('UTC'))
    // E não pode ser o de outro fuso qualquer: numa máquina fora do UTC, deixar de fixar o
    // fuso faria esta linha passar e a de cima falhar.
    expect(em('America/Sao_Paulo')).not.toBe(em('UTC'))
    expect(formatCaptureDate(ms)).not.toBe(em('America/Sao_Paulo'))
    expect(formatCaptureDate(0)).toBe('—')
  })
})

describe('formatDuration', () => {
  it('escolhe a unidade pela ordem de grandeza', () => {
    expect(formatDuration(45)).toBe('45s')
    expect(formatDuration(125)).toBe('2min 5s')
    expect(formatDuration(3 * 3600 + 25 * 60)).toBe('3h 25min')
    expect(formatDuration(3 * 86400 + 4 * 3600)).toBe('3d 4h')
  })

  it('não imprime notação científica quando a conta explode', () => {
    // Previsão de término com uma velocidade perto de zero: antes saía "2.16e+68h 37min".
    expect(formatDuration(7.79e71)).toBe('—')
    expect(formatDuration(Infinity)).toBe('—')
    expect(formatDuration(NaN)).toBe('—')
  })
})
