import { describe, expect, it } from 'vitest'
import { moveActive, normalizeQuery } from '@/utils/searchQuery'

describe('normalizeQuery', () => {
  it('trims and rejects blanks', () => {
    expect(normalizeQuery('  hello ')).toBe('hello')
    expect(normalizeQuery(' '.repeat(3))).toBeNull()
    expect(normalizeQuery('')).toBeNull()
  })
})

describe('moveActive', () => {
  it('wraps in both directions', () => {
    expect(moveActive(0, -1, 3)).toBe(2)
    expect(moveActive(2, 1, 3)).toBe(0)
    expect(moveActive(1, 1, 3)).toBe(2)
  })
  it('starts at the ends from no selection', () => {
    expect(moveActive(-1, 1, 3)).toBe(0)
    expect(moveActive(-1, -1, 3)).toBe(2)
  })
  it('is -1 for an empty list', () => expect(moveActive(0, 1, 0)).toBe(-1))
})
