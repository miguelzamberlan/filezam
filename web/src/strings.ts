// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Textos da interface. pt-BR é a referência (i18n/pt-BR.ts); en.ts e es.ts precisam ter as mesmas chaves.
// `S` é um objeto mutável preenchido por applyLocale() antes do primeiro render: os componentes
// leem S.chave na hora de renderizar, e constantes de módulo exigem recarregar a página
// (o diálogo de configurações faz isso ao trocar o idioma).
import { ptBR } from './i18n/pt-BR'
import { en } from './i18n/en'
import { es } from './i18n/es'

export type Strings = typeof ptBR
export type Locale = 'pt-BR' | 'en' | 'es'
export type LangPref = 'auto' | Locale

export const LOCALES: Record<Locale, Strings> = { 'pt-BR': ptBR, en, es }
export const LOCALE_NAMES: Record<Locale, string> = { 'pt-BR': 'Português (Brasil)', en: 'English', es: 'Español' }

export const S: Strings = { ...ptBR }

export function resolveLocale(pref: LangPref): Locale {
  if (pref !== 'auto') return pref
  const n = (typeof navigator !== 'undefined' ? navigator.language : '') || ''
  const l = n.toLowerCase()
  return l.startsWith('pt') ? 'pt-BR' : l.startsWith('es') ? 'es' : 'en'
}

export let currentLocale: Locale = 'pt-BR'

export function applyLocale(l: Locale) {
  currentLocale = l
  Object.assign(S, LOCALES[l])
  if (typeof document !== 'undefined') document.documentElement.lang = l
}

export function errorMessage(code: string | undefined, fallback?: string): string {
  return (code && S.errorCodes[code]) || fallback || S.errorCodes.internal
}

// Traduz um texto de erro ou aviso de job gravado pelo servidor (ver S.jobTexts).
export function jobText(msg: string): string {
  for (const [re, fn] of S.jobTexts) {
    const m = msg.match(re)
    if (m) return fn(m)
  }
  return msg
}
