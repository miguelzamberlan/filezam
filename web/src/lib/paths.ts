export function join(...parts: string[]): string {
  return parts.filter((p) => p !== '').join('/')
}

export function dirname(p: string): string {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(0, i) : ''
}

export function basename(p: string): string {
  const i = p.lastIndexOf('/')
  return i >= 0 ? p.slice(i + 1) : p
}

export function segments(p: string): string[] {
  return p === '' ? [] : p.split('/')
}

/** Encode a scope-relative path for use inside the browser URL (/b/...). */
export function encodePath(p: string): string {
  return segments(p).map(encodeURIComponent).join('/')
}

export function decodePath(p: string): string {
  return p
    .split('/')
    .filter((s) => s !== '')
    .map((s) => {
      try {
        return decodeURIComponent(s)
      } catch {
        return s
      }
    })
    .join('/')
}

export function isWithin(parent: string, child: string): boolean {
  return parent === '' || child === parent || child.startsWith(parent + '/')
}

/** Make "name (n).ext" not present in `taken`. */
export function uniqueName(name: string, taken: Set<string>): string {
  if (!taken.has(name)) return name
  const i = name.lastIndexOf('.')
  const [base, ext] = i > 0 ? [name.slice(0, i), name.slice(i)] : [name, '']
  for (let n = 1; n < 10000; n++) {
    const cand = `${base} (${n})${ext}`
    if (!taken.has(cand)) return cand
  }
  return `${base}-${Date.now()}${ext}`
}
