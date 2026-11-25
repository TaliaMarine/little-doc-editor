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

// Delete / lock / add-sub-page: editor+ space members only (Decision 46). A
// share-link guest has no role (null), so an edit link never unlocks these.
export function canManagePage (page: Pick<Page, 'my_role'>): boolean {
  return roleAtLeast(page.my_role, 'editor')
}

// Share links (create/list/revoke) and their access log: editor+ space role,
// members only (Decisions 16, 24). Guests have no role, so never.
export function canManageShareLinks (page: Pick<Page, 'my_role'>): boolean {
  return roleAtLeast(page.my_role, 'editor')
}

// Revision history is browsable by any space member (viewer+, Decision 30);
// share-link guests have no role and never see it (Decision 42).
export function canViewHistory (page: Pick<Page, 'my_role'>): boolean {
  return roleAtLeast(page.my_role, 'viewer')
}

// Restore stays editor+ (Decision 30) and is refused on locked pages.
export function canRestoreRevision (page: Pick<Page, 'my_role' | 'locked'>): boolean {
  return roleAtLeast(page.my_role, 'editor') && !page.locked
}
