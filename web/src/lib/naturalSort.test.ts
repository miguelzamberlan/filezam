import { describe, expect, it } from 'vitest'
import { sortEntries } from './naturalSort'
import type { Entry } from '../api/types'

const e = (name: string, type: Entry['type'] = 'file', size = 0, mtime = 0): Entry => ({ name, type, size, mtime })

describe('sortEntries', () => {
  it('folders first, natural numeric order', () => {
    const r = sortEntries([e('file10.txt'), e('file2.txt'), e('Zeta', 'dir'), e('alpha', 'dir')], { key: 'name', dir: 'asc' })
    expect(r.map((x) => x.name)).toEqual(['alpha', 'Zeta', 'file2.txt', 'file10.txt'])
  })
  it('size desc keeps folders first', () => {
    const r = sortEntries([e('a', 'file', 1), e('b', 'file', 5), e('d', 'dir')], { key: 'size', dir: 'desc' })
    expect(r.map((x) => x.name)).toEqual(['d', 'b', 'a'])
  })
})
