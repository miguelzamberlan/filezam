import { describe, it, expect } from 'vitest'
import { formatBytes, formatSpeed } from './format'

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
