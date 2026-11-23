import type { SpaceRole as ApiSpaceRole, Page } from '@/api/generated'

export type SpaceRole = ApiSpaceRole

// Mirrors api/internal/authz's ordering (Decision 24). Admin and owner are
// the same tier for every check the server makes; they're ranked here only
// so roleAtLeast(..., 'admin') reads naturally.
const RANK: Record<SpaceRole, number> = {
  viewer: 0,
  editor: 1,
  admin: 2,
  owner: 3,
}

// UI gating only — the server enforces roles regardless (SPEC.md §6.5). Use
// these to *hide* actions the caller can't perform (Decision 30's principle).
export function roleAtLeast (role: SpaceRole | null | undefined, min: SpaceRole): boolean {
  return role != null && RANK[role] >= RANK[min]
}

// A page's content is editable by the caller only if they have edit access
// (space editor+, or an edit share link) AND the page isn't locked — locked
// blocks writes for everyone, including org admins.
export function canEditPage (page: Pick<Page, 'can_edit' | 'locked'>): boolean {
  return page.can_edit && !page.locked
}
