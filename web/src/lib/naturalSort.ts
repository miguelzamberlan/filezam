import type { Entry } from '../api/types'

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

export type SortKey = 'name' | 'size' | 'mtime' | 'type'
export interface Sort {
  key: SortKey
  dir: 'asc' | 'desc'
}

export function compareNames(a: string, b: string): number {
  return collator.compare(a, b)
}

/** Folders first, then by the given key. */
export function sortEntries(entries: Entry[], sort: Sort): Entry[] {
  const mul = sort.dir === 'asc' ? 1 : -1
  return [...entries].sort((a, b) => {
    const ad = a.type === 'dir' ? 0 : 1
    const bd = b.type === 'dir' ? 0 : 1
    if (ad !== bd) return ad - bd
    let c = 0
    switch (sort.key) {
      case 'size':
        c = a.size - b.size
        break
      case 'mtime':
        c = a.mtime - b.mtime
        break
      case 'type':
        c = compareNames(extKey(a), extKey(b))
        break
    }
    if (c === 0) c = compareNames(a.name, b.name) * (sort.key === 'name' ? 1 : mul)
    return c * mul
  })
}

function extKey(e: Entry): string {
  if (e.type === 'dir') return ''
  const i = e.name.lastIndexOf('.')
  return i > 0 ? e.name.slice(i + 1).toLowerCase() : ''
}
