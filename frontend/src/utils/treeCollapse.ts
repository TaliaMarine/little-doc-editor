// Per-user, per-space collapsed-group state for the page tree, kept in
// localStorage (no server round trip; it's a view preference).
const PREFIX = 'little-doc-editor.tree-collapsed'

function key (userKey: string, spaceId: string): string {
  return `${PREFIX}.${userKey}.${spaceId}`
}

export function loadCollapsed (userKey: string, spaceId: string): Set<string> {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(key(userKey, spaceId)) ?? '[]')
    return new Set(Array.isArray(parsed) ? parsed.filter((id): id is string => typeof id === 'string') : [])
  } catch {
    return new Set()
  }
}

export function saveCollapsed (userKey: string, spaceId: string, collapsed: Set<string>): void {
  try {
    if (collapsed.size === 0) {
      localStorage.removeItem(key(userKey, spaceId))
    } else {
      localStorage.setItem(key(userKey, spaceId), JSON.stringify([...collapsed]))
    }
  } catch {
    // Storage unavailable/full: the preference just doesn't persist.
  }
}

// Rows visible given collapsed groups (a row hides if any ancestor is
// collapsed). `items` is the depth-first flattened tree.
export function visibleRows<T extends { id: string, depth: number }> (items: T[], collapsed: Set<string>): T[] {
  const result: T[] = []
  let hideBelow: number | null = null
  for (const item of items) {
    if (hideBelow !== null) {
      if (item.depth > hideBelow) {
        continue
      }
      hideBelow = null
    }
    result.push(item)
    if (collapsed.has(item.id)) {
      hideBelow = item.depth
    }
  }
  return result
}
