import { describe, expect, it } from 'vitest'
import { planDrop, planMoveToParent, planStep, positionAt, subtreeIds, validParents, zoneFromOffset } from '@/utils/pageMove'

const n = (id: string, parent_id: string | null, position: number) => ({ id, parent_id, position })
const tree = [n('a', null, 1), n('b', null, 2), n('c', null, 4), n('a1', 'a', 1), n('a2', 'a', 3)]

describe('positionAt', () => {
  it('handles empty, ends and midpoints', () => {
    expect(positionAt([], 0)).toBe(1)
    expect(positionAt([{ position: 5 }], 0)).toBe(4)
    expect(positionAt([{ position: 5 }], 1)).toBe(6)
    expect(positionAt([{ position: 1 }, { position: 2 }], 1)).toBe(1.5)
  })

  it('reports an exhausted gap', () => {
    const a = 1
    const b = 1 + Number.EPSILON
    expect(positionAt([{ position: a }, { position: b }], 1)).toBeNull()
  })
})

describe('planDrop', () => {
  it('places before/after a sibling with the midpoint', () => {
    expect(planDrop(tree, 'c', 'b', 'before')).toEqual({ parentId: null, position: 1.5 })
    expect(planDrop(tree, 'a', 'c', 'after')).toEqual({ parentId: null, position: 5 })
    expect(planDrop(tree, 'c', 'a', 'before')).toEqual({ parentId: null, position: 0 })
  })

  it('ignores the dragged page when computing flanks', () => {
    // b is moved after a: flanks are a(1) and c(4), not b itself.
    expect(planDrop(tree, 'b', 'a', 'after')).toEqual({ parentId: null, position: 2.5 })
  })

  it('reparents inside a target as last child', () => {
    expect(planDrop(tree, 'c', 'a', 'inside')).toEqual({ parentId: 'a', position: 4 })
    expect(planDrop(tree, 'c', 'b', 'inside')).toEqual({ parentId: 'b', position: 1 })
  })

  it('reparents next to a nested target', () => {
    expect(planDrop(tree, 'c', 'a2', 'before')).toEqual({ parentId: 'a', position: 2 })
  })

  it('rejects self, descendants and unknown pages (other space)', () => {
    expect(planDrop(tree, 'a', 'a', 'inside')).toBeNull()
    expect(planDrop(tree, 'a', 'a1', 'inside')).toBeNull()
    expect(planDrop(tree, 'a', 'a2', 'before')).toBeNull()
    expect(planDrop(tree, 'a', 'zzz', 'before')).toBeNull()
    expect(planDrop(tree, 'zzz', 'a', 'before')).toBeNull()
  })
})

describe('planMoveToParent / planStep / validParents', () => {
  it('moves to root or a parent', () => {
    expect(planMoveToParent(tree, 'a1', null)).toEqual({ parentId: null, position: 5 })
    expect(planMoveToParent(tree, 'a', 'a1')).toBeNull()
  })

  it('steps among siblings', () => {
    expect(planStep(tree, 'c', 'up')).toEqual({ parentId: null, position: 1.5 })
    expect(planStep(tree, 'a', 'up')).toBeNull()
    expect(planStep(tree, 'c', 'down')).toBeNull()
    expect(planStep(tree, 'a1', 'down')).toEqual({ parentId: 'a', position: 4 })
  })

  it('excludes the subtree from valid parents', () => {
    expect(validParents(tree, 'a').map(i => i.id)).toEqual(['b', 'c'])
    expect([...subtreeIds(tree, 'a')].toSorted()).toEqual(['a', 'a1', 'a2'])
  })
})

describe('zoneFromOffset', () => {
  it('splits a row in thirds-ish', () => {
    expect(zoneFromOffset(1, 40)).toBe('before')
    expect(zoneFromOffset(20, 40)).toBe('inside')
    expect(zoneFromOffset(39, 40)).toBe('after')
  })
})
