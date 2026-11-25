import type { SpaceRole } from '@/utils/permissions'
import { describe, expect, it } from 'vitest'
import { canManageShareLinks, canRestoreRevision, canViewHistory } from '@/utils/permissions'

const page = (my_role: SpaceRole | null, locked = false) => ({ my_role, locked })

describe('history/share gating', () => {
  it('lets every member browse history but never guests', () => {
    for (const role of ['viewer', 'editor', 'admin', 'owner'] as const) {
      expect(canViewHistory(page(role))).toBe(true)
    }
    expect(canViewHistory(page(null))).toBe(false)
  })

  it('restores only for editor+ on unlocked pages', () => {
    expect(canRestoreRevision(page('viewer'))).toBe(false)
    expect(canRestoreRevision(page(null))).toBe(false)
    expect(canRestoreRevision(page('editor'))).toBe(true)
    expect(canRestoreRevision(page('editor', true))).toBe(false)
  })

  it('manages share links for editor+ members only', () => {
    expect(canManageShareLinks(page('viewer'))).toBe(false)
    expect(canManageShareLinks(page(null))).toBe(false)
    expect(canManageShareLinks(page('editor'))).toBe(true)
    expect(canManageShareLinks(page('owner'))).toBe(true)
  })
})
