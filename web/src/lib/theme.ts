import { useUI, type Prefs } from '../store/ui'

export type Theme = 'system' | 'light' | 'dark'

export const DEFAULT_COLORS = { accent: '#2563eb', selection: '#3b82f6', focus: '#3b82f6' } as const
export type ColorKey = keyof typeof DEFAULT_COLORS

// Presets: [rótulo, destaque, seleção, foco]
export const COLOR_PRESETS: { name: string; colors: Record<ColorKey, string> }[] = [
  { name: 'Azul', colors: { ...DEFAULT_COLORS } },
  { name: 'Verde', colors: { accent: '#059669', selection: '#10b981', focus: '#10b981' } },
  { name: 'Roxo', colors: { accent: '#7c3aed', selection: '#8b5cf6', focus: '#8b5cf6' } },
  { name: 'Laranja', colors: { accent: '#ea580c', selection: '#f97316', focus: '#f97316' } },
  { name: 'Rosa', colors: { accent: '#db2777', selection: '#ec4899', focus: '#ec4899' } },
  { name: 'Cinza', colors: { accent: '#4b5563', selection: '#6b7280', focus: '#9ca3af' } },
]

const HEX = /^#[0-9a-f]{6}$/i
export const validColor = (v: unknown, fallback: string) => (typeof v === 'string' && HEX.test(v) ? v : fallback)

const mq = window.matchMedia('(prefers-color-scheme: dark)')

// applyTheme escreve a classe .dark e as variáveis de cor no <html>.
export function applyTheme(p: Prefs) {
  const root = document.documentElement
  const dark = p.theme === 'dark' || (p.theme !== 'light' && mq.matches)
  root.classList.toggle('dark', dark)
  root.style.colorScheme = dark ? 'dark' : 'light' // barras de rolagem e controles nativos acompanham
  for (const k of Object.keys(DEFAULT_COLORS) as ColorKey[]) root.style.setProperty('--' + k, validColor(p[k], DEFAULT_COLORS[k]))
}

// initTheme aplica na carga e reaplica a cada mudança de preferência ou do tema do sistema.
export function initTheme() {
  applyTheme(useUI.getState().prefs)
  useUI.subscribe((s, prev) => s.prefs !== prev.prefs && applyTheme(s.prefs))
  mq.addEventListener('change', () => applyTheme(useUI.getState().prefs))
}
