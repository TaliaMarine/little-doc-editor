import { describe, expect, it } from 'vitest'
import { canEditPage, roleAtLeast } from '@/utils/permissions'

describe('roleAtLeast', () => {
  it('treats a missing role as no access', () => {
    expect(roleAtLeast(null, 'viewer')).toBe(false)
    expect(roleAtLeast(undefined, 'viewer')).toBe(false)
  })

  it('orders viewer < editor < admin < owner', () => {
    expect(roleAtLeast('viewer', 'viewer')).toBe(true)
    expect(roleAtLeast('viewer', 'editor')).toBe(false)
    expect(roleAtLeast('editor', 'editor')).toBe(true)
    expect(roleAtLeast('editor', 'admin')).toBe(false)
    expect(roleAtLeast('admin', 'editor')).toBe(true)
    expect(roleAtLeast('owner', 'admin')).toBe(true)
  })
})

describe('canEditPage', () => {
  it('requires can_edit', () => {
    expect(canEditPage({ can_edit: false, locked: false })).toBe(false)
    expect(canEditPage({ can_edit: true, locked: false })).toBe(true)
  })

  it('is false for a locked page even with edit access', () => {
    expect(canEditPage({ can_edit: true, locked: true })).toBe(false)
  })
})
