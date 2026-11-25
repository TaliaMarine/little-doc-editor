// Search helpers kept free of Vue so they unit-test directly.
export const SEARCH_DEBOUNCE_MS = 250

// Trimmed query, or null when there is nothing to search for (the api 400s on
// an empty `q`).
export function normalizeQuery (raw: string): string | null {
  const q = raw.trim()
  return q === '' ? null : q
}

// Next index in a result list for ArrowUp/ArrowDown, wrapping at both ends.
export function moveActive (current: number, delta: 1 | -1, length: number): number {
  if (length === 0) {
    return -1
  }
  if (current < 0) {
    return delta === 1 ? 0 : length - 1
  }
  return (current + delta + length) % length
}
