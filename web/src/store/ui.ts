import { create } from 'zustand'
import type { Sort } from '../lib/naturalSort'

export interface Clipboard {
  op: 'copy' | 'cut'
  dir: string
  names: string[]
}

export interface Prefs {
  showHidden: boolean
  showHints: boolean
  confirmDelete: boolean
  zoom: number // fator aplicado à listagem (CSS zoom); 1 = padrão
  theme: 'system' | 'light' | 'dark'
  accent: string // cores em hex (#rrggbb); ver lib/theme.ts
  selection: string
  focus: string
}

export const ZOOM_STEPS = [0.85, 1, 1.15, 1.3, 1.5, 1.75, 2]

const defaultPrefs: Prefs = {
  showHidden: false, showHints: true, confirmDelete: true, zoom: 1,
  theme: 'system', accent: '#2563eb', selection: '#3b82f6', focus: '#3b82f6',
}

interface UIState {
  prefs: Prefs
  setPrefs: (p: Partial<Prefs>) => void
  selection: Set<string>
  anchor: string | null
  focused: string | null
  clipboard: Clipboard | null
  sort: Sort
  view: 'list' | 'grid'
  filter: string
  uploadPanelOpen: boolean // lista do painel de envios expandida
  setUploadPanelOpen: (v: boolean) => void
  sidebarOpen: boolean // menu lateral aberto (só em telas estreitas)
  setSidebarOpen: (v: boolean) => void
  setSelection: (s: Set<string>, anchor?: string | null, focused?: string | null) => void
  clearSelection: () => void
  setFocused: (name: string | null) => void
  setClipboard: (c: Clipboard | null) => void
  setSort: (s: Sort) => void
  setView: (v: 'list' | 'grid') => void
  setFilter: (f: string) => void
}

function load<T>(key: string, def: T): T {
  try {
    const v = localStorage.getItem(key)
    return v ? (JSON.parse(v) as T) : def
  } catch {
    return def
  }
}

function persist(key: string, v: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(v))
  } catch {
    /* ignore */
  }
}

export const useUI = create<UIState>((set) => ({
  prefs: { ...defaultPrefs, ...load<Partial<Prefs>>('filezam.prefs', {}) },
  setPrefs: (p) =>
    set((st) => {
      const prefs = { ...st.prefs, ...p }
      persist('filezam.prefs', prefs)
      return { prefs }
    }),
  selection: new Set(),
  anchor: null,
  focused: null,
  clipboard: null,
  sort: load<Sort>('filezam.sort', { key: 'name', dir: 'asc' }),
  view: load<'list' | 'grid'>('filezam.view', 'list'),
  filter: '',
  uploadPanelOpen: true,
  setUploadPanelOpen: (uploadPanelOpen) => set({ uploadPanelOpen }),
  sidebarOpen: false,
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  setSelection: (selection, anchor, focused) =>
    set((st) => ({ selection, anchor: anchor === undefined ? st.anchor : anchor, focused: focused === undefined ? st.focused : focused })),
  clearSelection: () => set({ selection: new Set(), anchor: null }),
  setFocused: (focused) => set({ focused }),
  setClipboard: (clipboard) => set({ clipboard }),
  setSort: (sort) => {
    persist('filezam.sort', sort)
    set({ sort })
  },
  setView: (view) => {
    persist('filezam.view', view)
    set({ view })
  },
  setFilter: (filter) => set({ filter }),
}))
