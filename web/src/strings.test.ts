// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { describe, expect, it } from 'vitest'
import { applyLocale, jobText, resolveLocale, S } from './strings'

describe('jobText', () => {
  it('traduz os textos de job gravados pelo servidor', () => {
    applyLocale('pt-BR')
    expect(jobText('interrupted by server restart; partial copy of 5 files: the finished ones were kept, the unfinished one was removed')).toContain('Cópia parcial de 5 arquivos')
    expect(jobText('interrupted by server restart; the unfinished copy was removed')).toContain('nada ficou no destino')
    expect(jobText('interrupted by server restart')).toBe('Interrompida pelo reinício do servidor.')
    expect(jobText('partial copy: 3 of 4 files copied; the finished ones were kept, the unfinished one was removed')).toContain('3 de 4')
  })
  it('deixa passar o que não conhece', () => {
    expect(jobText('something the server never said')).toBe('something the server never said')
  })
  it('segue o idioma ativo', () => {
    applyLocale('en')
    expect(jobText('interrupted by server restart; the partial extraction was removed')).toBe('Interrupted by a server restart. The partial extraction was removed.')
    applyLocale('pt-BR')
  })
  it('espanhol', () => {
    applyLocale('es')
    expect(S.login).toBe('Iniciar sesión')
    expect(jobText('interrupted by server restart; the unfinished copy was removed')).toContain('Interrumpida por el reinicio del servidor')
    applyLocale('pt-BR')
  })
})

describe('resolveLocale', () => {
  it('escolhe pelo idioma do navegador quando a preferência é auto', () => {
    const nav = (lang: string) => Object.defineProperty(globalThis, 'navigator', { value: { language: lang }, configurable: true })
    nav('es-MX')
    expect(resolveLocale('auto')).toBe('es')
    nav('pt-PT')
    expect(resolveLocale('auto')).toBe('pt-BR')
    nav('de-DE')
    expect(resolveLocale('auto')).toBe('en')
    expect(resolveLocale('es')).toBe('es')
  })
})
