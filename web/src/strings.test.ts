import { describe, expect, it } from 'vitest'
import { applyLocale, jobText } from './strings'

describe('jobText', () => {
  it('traduz os textos de job gravados pelo servidor', () => {
    applyLocale('pt-BR')
    expect(jobText('interrupted by server restart; partial copy of 5 files: the finished ones were kept, the unfinished one was removed')).toContain('Cópia parcial de 5 arquivos')
    expect(jobText('interrupted by server restart; the unfinished copy was removed')).toContain('nada ficou no destino')
    expect(jobText('interrupted by server restart')).toBe('Interrompida pelo reinício do servidor.')
    expect(jobText('partial copy: 3 of 4 files copied; the finished ones were kept, the unfinished one was removed')).toContain('3 de 4')
  })
  it('deixa passar o que não conhece', () => {
    expect(jobText('archive too large to extract')).toBe('archive too large to extract')
  })
  it('segue o idioma ativo', () => {
    applyLocale('en')
    expect(jobText('interrupted by server restart; the partial extraction was removed')).toBe('Interrupted by a server restart. The partial extraction was removed.')
    applyLocale('pt-BR')
  })
})
