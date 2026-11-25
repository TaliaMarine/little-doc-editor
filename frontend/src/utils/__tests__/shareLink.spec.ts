import { describe, expect, it } from 'vitest'
import { isTokenExpired } from '@/utils/jwt'
import { expiryToIso, shareLinkStatus, shareLinkUrl, sharePathForHref } from '@/utils/shareLink'

const ID = '11111111-2222-3333-4444-555555555555'

describe('shareLink utils', () => {
  it('builds the public url from the token only', () => {
    expect(shareLinkUrl('https://x.test', 'abc_DEF-1')).toBe('https://x.test/share/abc_DEF-1')
  })

  it('derives status', () => {
    const now = Date.parse('2030-01-01T00:00:00Z')
    expect(shareLinkStatus({ revoked_at: null, expires_at: null }, now)).toBe('active')
    expect(shareLinkStatus({ revoked_at: null, expires_at: '2029-01-01T00:00:00Z' }, now)).toBe('expired')
    expect(shareLinkStatus({ revoked_at: '2029-01-01T00:00:00Z', expires_at: null }, now)).toBe('revoked')
    expect(shareLinkStatus({ revoked_at: null, expires_at: '2031-01-01T00:00:00Z' }, now)).toBe('active')
  })

  it('only accepts future expiries', () => {
    const now = Date.parse('2030-01-01T00:00:00Z')
    expect(expiryToIso('', now)).toBeNull()
    expect(expiryToIso('garbage', now)).toBeNull()
    expect(expiryToIso('2020-01-01T00:00', now)).toBeNull()
    expect(expiryToIso('2035-06-15T12:00', now)).toMatch(/^2035-06-15/)
  })

  it('maps in-app page links into the share route, nothing else', () => {
    const o = 'https://x.test'
    expect(sharePathForHref(`/spaces/s1/pages/${ID}`, o, 'tok')).toBe(`/share/tok/${ID}`)
    expect(sharePathForHref(`https://x.test/share/other/${ID}`, o, 'tok')).toBe(`/share/tok/${ID}`)
    expect(sharePathForHref(`https://evil.test/spaces/s1/pages/${ID}`, o, 'tok')).toBeNull()
    expect(sharePathForHref('https://x.test/admin', o, 'tok')).toBeNull()
    expect(sharePathForHref('javascript:alert(1)', o, 'tok')).toBeNull()
  })
})

describe('isTokenExpired', () => {
  const jwt = (exp: unknown) => `h.${btoa(JSON.stringify({ exp })).replaceAll('=', '')}.s`
  it('reads exp', () => {
    expect(isTokenExpired(jwt(Math.floor(Date.now() / 1000) + 60))).toBe(false)
    expect(isTokenExpired(jwt(1))).toBe(true)
    expect(isTokenExpired('nope')).toBe(true)
    expect(isTokenExpired(jwt('x'))).toBe(true)
  })
})
