import { describe, expect, it } from 'vitest'
import { basename, dirname, encodePath, decodePath, join, uniqueName } from './paths'

describe('paths', () => {
  it('join/dirname/basename', () => {
    expect(join('', 'a', 'b')).toBe('a/b')
    expect(dirname('a/b/c')).toBe('a/b')
    expect(dirname('a')).toBe('')
    expect(basename('a/b')).toBe('b')
  })
  it('encode roundtrip', () => {
    const p = 'pasta ção/arq #1?.txt'
    expect(decodePath(encodePath(p))).toBe(p)
    expect(encodePath(p)).not.toContain('#')
  })
  it('uniqueName', () => {
    expect(uniqueName('a.txt', new Set())).toBe('a.txt')
    expect(uniqueName('a.txt', new Set(['a.txt']))).toBe('a (1).txt')
    expect(uniqueName('a.txt', new Set(['a.txt', 'a (1).txt']))).toBe('a (2).txt')
    expect(uniqueName('noext', new Set(['noext']))).toBe('noext (1)')
  })
})
