import { create } from 'zustand'
import type { Sort } from '../lib/naturalSort'

export interface Clipboard {
  op: 'copy' | 'cut'
  dir: string
  names: string[]
}

interface UIState {
  selection: Set<string>
  anchor: string | null
  focused: string | null
  clipboard: Clipboard | null
  sort: Sort
  view: 'list' | 'grid'
  filter: string
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
  selection: new Set(),
  anchor: null,
  focused: null,
  clipboard: null,
  sort: load<Sort>('filezam.sort', { key: 'name', dir: 'asc' }),
  view: load<'list' | 'grid'>('filezam.view', 'list'),
  filter: '',
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
