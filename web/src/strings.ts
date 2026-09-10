// Textos da interface. pt-BR é a referência (i18n/pt-BR.ts); en.ts precisa ter as mesmas chaves.
// `S` é um objeto mutável preenchido por applyLocale() antes do primeiro render: os componentes
// leem S.chave na hora de renderizar, e constantes de módulo exigem recarregar a página
// (o diálogo de configurações faz isso ao trocar o idioma).
import { ptBR } from './i18n/pt-BR'
import { en } from './i18n/en'

export type Strings = typeof ptBR
export type Locale = 'pt-BR' | 'en'
export type LangPref = 'auto' | Locale

export const LOCALES: Record<Locale, Strings> = { 'pt-BR': ptBR, en }
export const LOCALE_NAMES: Record<Locale, string> = { 'pt-BR': 'Português (Brasil)', en: 'English' }

export const S: Strings = { ...ptBR }

export function resolveLocale(pref: LangPref): Locale {
  if (pref !== 'auto') return pref
  const n = (typeof navigator !== 'undefined' ? navigator.language : '') || ''
  return n.toLowerCase().startsWith('pt') ? 'pt-BR' : 'en'
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
