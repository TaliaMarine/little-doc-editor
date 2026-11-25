import { describe, expect, it } from 'vitest'
import { safeRedirect } from '@/utils/redirect'

describe('safeRedirect', () => {
  it('keeps same-app paths', () => {
    expect(safeRedirect('/spaces/1?x=2')).toBe('/spaces/1?x=2')
  })
  it('rejects external and callback targets', () => {
    for (const bad of ['//evil.com', String.raw`/\evil.com`, 'https://evil.com', 'javascript:1', undefined, ['/a'], '/auth/callback#token=x', '/auth/callback?x=1']) {
      expect(safeRedirect(bad)).toBe('/')
    }
  })
  it('drops fragments', () => {
    expect(safeRedirect('/a#frag')).toBe('/a')
  })
})
