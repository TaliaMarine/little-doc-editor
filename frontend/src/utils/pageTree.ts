import type { PageTreeItem } from '@/api/generated'

export type FlatPageTreeItem = PageTreeItem & { depth: number }

// Flattens the API's position-ordered page list into depth-first order with a
// depth per row. Grouped by parent in one pass (O(n)); cycle-safe.
export function flattenPageTree (items: PageTreeItem[]): FlatPageTreeItem[] {
  const byParent = new Map<string | null, PageTreeItem[]>()
  for (const item of items) {
    const siblings = byParent.get(item.parent_id)
    if (siblings) {
      siblings.push(item)
    } else {
      byParent.set(item.parent_id, [item])
    }
  }
  const result: FlatPageTreeItem[] = []
  const visited = new Set<string>()
  function walk (parentId: string | null, depth: number) {
    for (const item of byParent.get(parentId) ?? []) {
      if (visited.has(item.id)) {
        continue
      }
      visited.add(item.id)
      result.push({ ...item, depth })
      walk(item.id, depth + 1)
    }
  }
  walk(null, 0)
  return result
}
