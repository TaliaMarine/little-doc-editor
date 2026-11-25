import { describe, expect, it } from 'vitest'
import {
  assignableRoles,
  bytesToMib,
  canChangeMemberRole,
  expiryFromDays,
  formatAllowlist,
  inviteLink,
  mibToBytes,
  parseAllowlist,
  parseOptionalPositiveInt,
  redeemErrorKey,
  slugify,
} from '@/utils/accounts'
import { canManagePage } from '@/utils/permissions'

describe('invite helpers', () => {
  it('builds the one-time link from origin and token', () => {
    expect(inviteLink('https://docs.example.com', 'abc_-123')).toBe('https://docs.example.com/invite/abc_-123')
  })

  it('computes expiry from days and omits it when unset', () => {
    const now = new Date('2026-01-01T00:00:00Z')
    expect(expiryFromDays(7, now)).toBe('2026-01-08T00:00:00.000Z')
    expect(expiryFromDays(null, now)).toBeUndefined()
    expect(expiryFromDays(0, now)).toBeUndefined()
    expect(expiryFromDays(Number.NaN, now)).toBeUndefined()
  })

  it('maps redeem statuses to messages', () => {
    expect(redeemErrorKey(410, 'resolve')).toBe('redeem.gone')
    expect(redeemErrorKey(409, 'redeem')).toBe('redeem.emailTaken')
    expect(redeemErrorKey(429, 'redeem')).toBe('redeem.rateLimited')
    expect(redeemErrorKey(404, 'resolve')).toBe('redeem.invalid')
    expect(redeemErrorKey(500, 'redeem')).toBe('redeem.failed')
  })
})

describe('member role rules', () => {
  it('offers nothing below admin', () => {
    expect(assignableRoles('editor')).toEqual([])
    expect(assignableRoles('viewer')).toEqual([])
    expect(assignableRoles(null)).toEqual([])
  })

  it('only owners may grant owner (Decision 46)', () => {
    expect(assignableRoles('admin')).toEqual(['admin', 'editor', 'viewer'])
    expect(assignableRoles('owner')).toEqual(['owner', 'admin', 'editor', 'viewer'])
  })

  it('stops admins from changing or removing an owner row', () => {
    expect(canChangeMemberRole('admin', 'owner')).toBe(false)
    expect(canChangeMemberRole('admin', 'editor')).toBe(true)
    expect(canChangeMemberRole('owner', 'owner')).toBe(true)
    expect(canChangeMemberRole('editor', 'viewer')).toBe(false)
  })
})

describe('admin settings helpers', () => {
  it('parses allowlists, dropping blanks and duplicates', () => {
    expect(parseAllowlist('image/png\n\n image/jpeg ,image/png')).toEqual(['image/png', 'image/jpeg'])
    expect(parseAllowlist('  \n ')).toEqual([])
    expect(parseAllowlist(formatAllowlist(['a/b', 'c/d']))).toEqual(['a/b', 'c/d'])
  })

  it('converts MiB and bytes', () => {
    expect(mibToBytes(25)).toBe(26_214_400)
    expect(bytesToMib(26_214_400)).toBe(25)
    expect(bytesToMib(1_500_000)).toBe(1.43)
  })

  it('parses optional positive ints (blank = null, invalid = undefined)', () => {
    expect(parseOptionalPositiveInt('')).toBeNull()
    expect(parseOptionalPositiveInt('  ')).toBeNull()
    expect(parseOptionalPositiveInt('30')).toBe(30)
    expect(parseOptionalPositiveInt('0')).toBeUndefined()
    expect(parseOptionalPositiveInt('1.5')).toBeUndefined()
    expect(parseOptionalPositiveInt('abc')).toBeUndefined()
  })

  it('slugifies names', () => {
    expect(slugify('Ünïcode  Engineering!')).toBe('unicode-engineering')
  })
})

describe('canManagePage', () => {
  it('needs an editor+ membership; guests and viewers are refused', () => {
    expect(canManagePage({ my_role: 'viewer' })).toBe(false)
    expect(canManagePage({ my_role: null })).toBe(false)
    expect(canManagePage({ my_role: 'editor' })).toBe(true)
    expect(canManagePage({ my_role: 'owner' })).toBe(true)
  })
})
