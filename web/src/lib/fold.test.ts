import { describe, expect, it } from 'vitest'
import { fold } from './fold'

describe('fold', () => {
  it('ignora maiúsculas, acentos e cedilha como o servidor', () => {
    expect(fold('Relatório de AÇÃO.pdf')).toBe('relatorio de acao.pdf')
    expect(fold('Relato\u0301rio de Ac\u0327a\u0303o')).toBe('relatorio de acao') // NFD do macOS
    expect(fold('Crème Brûlée – Ñandú')).toBe('creme brulee – nandu')
    expect(fold('Straße ẞ')).toBe('strasse ss')
    expect(fold('Øresund Łódź Æsir Œuvre')).toBe('oresund lodz aesir oeuvre')
    expect(fold('İstanbul')).toBe('istanbul')
    expect(fold('ﬁcha ２０２６')).toBe('ficha 2026')
    expect(fold('日本語.txt')).toBe('日本語.txt')
  })
})
