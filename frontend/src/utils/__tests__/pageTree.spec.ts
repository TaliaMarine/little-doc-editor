import { describe, expect, it } from 'vitest'
import { flattenPageTree } from '@/utils/pageTree'

const item = (id: string, parent_id: string | null) => ({ id, parent_id, position: 0, title: id, locked: false, is_template: false, created_at: '', updated_at: '' })

describe('flattenPageTree', () => {
  it('orders depth-first with depths', () => {
    const flat = flattenPageTree([item('a', null), item('b', 'a'), item('c', null), item('d', 'b')])
    expect(flat.map(i => [i.id, i.depth])).toEqual([['a', 0], ['b', 1], ['d', 2], ['c', 0]])
  })

  it('does not loop on cycles', () => {
    expect(flattenPageTree([item('a', 'b'), item('b', 'a')])).toEqual([])
  })
})
