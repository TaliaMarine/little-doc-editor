import type { PageTreeItem } from '@/api/generated'

export type DropZone = 'before' | 'after' | 'inside'

export interface MovePlan {
  parentId: string | null
  position: number
}

type Positioned = Pick<PageTreeItem, 'id' | 'parent_id' | 'position'>

// Position of a page inserted at `index` among already-ordered siblings
// (the moved page itself must not be in `siblings`). The server persists
// whatever it is given, so this is the fractional midpoint between the two
// flanking siblings. Returns null when the gap has collapsed to float
// precision (the caller should tell the user instead of sending a position
// that would tie with a neighbour).
export function positionAt (siblings: Pick<PageTreeItem, 'position'>[], index: number): number | null {
  const prev = siblings[index - 1]?.position
  const next = siblings[index]?.position
  if (prev === undefined && next === undefined) {
    return 1
  }
  if (prev === undefined) {
    return next! - 1
  }
  if (next === undefined) {
    return prev + 1
  }
  const mid = prev + (next - prev) / 2
  return mid > prev && mid < next ? mid : null
}

function orderedChildren (items: Positioned[], parentId: string | null, excludeId: string): Positioned[] {
  return items
    .filter(item => item.parent_id === parentId && item.id !== excludeId)
    .toSorted((a, b) => a.position - b.position)
}

// Every id in the subtree rooted at `id`, including `id` (cycle-safe).
export function subtreeIds (items: Positioned[], id: string): Set<string> {
  const byParent = new Map<string, string[]>()
  for (const item of items) {
    if (item.parent_id) {
      byParent.set(item.parent_id, [...(byParent.get(item.parent_id) ?? []), item.id])
    }
  }
  const result = new Set<string>()
  const stack = [id]
  while (stack.length > 0) {
    const current = stack.pop()!
    if (result.has(current)) {
      continue
    }
    result.add(current)
    stack.push(...(byParent.get(current) ?? []))
  }
  return result
}

// Where does dropping `draggedId` on `targetId` (in `zone`) put it? null when
// the drop is invalid: onto itself or into its own subtree, or unknown ids.
// Ids outside `items` (i.e. another space) are rejected, so a same-space
// invariant holds by construction (the server enforces it too).
export function planDrop (items: Positioned[], draggedId: string, targetId: string, zone: DropZone): MovePlan | null {
  const target = items.find(item => item.id === targetId)
  if (!target || !items.some(item => item.id === draggedId) || subtreeIds(items, draggedId).has(targetId)) {
    return null
  }
  if (zone === 'inside') {
    return planMoveToParent(items, draggedId, targetId)
  }
  const siblings = orderedChildren(items, target.parent_id, draggedId)
  const index = siblings.findIndex(item => item.id === targetId) + (zone === 'after' ? 1 : 0)
  const position = positionAt(siblings, index)
  return position === null ? null : { parentId: target.parent_id, position }
}

// "Move to…": make `draggedId` the last child of `parentId` (null = root).
export function planMoveToParent (items: Positioned[], draggedId: string, parentId: string | null): MovePlan | null {
  if (parentId !== null && (!items.some(item => item.id === parentId) || subtreeIds(items, draggedId).has(parentId))) {
    return null
  }
  const siblings = orderedChildren(items, parentId, draggedId)
  const position = positionAt(siblings, siblings.length)
  return position === null ? null : { parentId, position }
}

// Keyboard-friendly one-step reorder among current siblings.
export function planStep (items: Positioned[], id: string, direction: 'up' | 'down'): MovePlan | null {
  const self = items.find(item => item.id === id)
  if (!self) {
    return null
  }
  const all = items
    .filter(item => item.parent_id === self.parent_id)
    .toSorted((a, b) => a.position - b.position)
  const at = all.findIndex(item => item.id === id)
  const neighbour = all[direction === 'up' ? at - 1 : at + 1]
  return neighbour ? planDrop(items, id, neighbour.id, direction === 'up' ? 'before' : 'after') : null
}

// Pages `id` may be moved under (everything except itself and its subtree).
export function validParents<T extends Positioned> (items: T[], id: string): T[] {
  const excluded = subtreeIds(items, id)
  return items.filter(item => !excluded.has(item.id))
}

// Which third of a row the pointer is over: top = before, bottom = after,
// middle = reparent under it.
export function zoneFromOffset (offsetY: number, height: number): DropZone {
  if (height <= 0) {
    return 'inside'
  }
  const ratio = offsetY / height
  if (ratio < 0.28) {
    return 'before'
  }
  return ratio > 0.72 ? 'after' : 'inside'
}
