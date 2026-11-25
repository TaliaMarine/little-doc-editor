import { beforeEach, describe, expect, it } from 'vitest'
import { loadCollapsed, saveCollapsed, visibleRows } from '@/utils/treeCollapse'

describe('treeCollapse', () => {
  beforeEach(() => localStorage.clear())

  it('persists per user and space', () => {
    saveCollapsed('u1', 's1', new Set(['a']))
    expect([...loadCollapsed('u1', 's1')]).toEqual(['a'])
    expect(loadCollapsed('u2', 's1').size).toBe(0)
    expect(loadCollapsed('u1', 's2').size).toBe(0)
    saveCollapsed('u1', 's1', new Set())
    expect(localStorage.length).toBe(0)
  })

  it('survives corrupt storage', () => {
    localStorage.setItem('little-doc-editor.tree-collapsed.u1.s1', '{nope')
    expect(loadCollapsed('u1', 's1').size).toBe(0)
  })

  it('hides descendants of collapsed rows only', () => {
    const rows = [
      { id: 'a', depth: 0 }, { id: 'b', depth: 1 }, { id: 'c', depth: 2 },
      { id: 'd', depth: 1 }, { id: 'e', depth: 0 }, { id: 'f', depth: 1 },
    ]
    expect(visibleRows(rows, new Set(['b'])).map(r => r.id)).toEqual(['a', 'b', 'd', 'e', 'f'])
    expect(visibleRows(rows, new Set(['a', 'e'])).map(r => r.id)).toEqual(['a', 'e'])
  })
})
