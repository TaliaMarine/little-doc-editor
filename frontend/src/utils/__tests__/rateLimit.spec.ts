import { describe, expect, it } from 'vitest'
import { rateLimitMessage, retryAfterSeconds } from '@/utils/rateLimit'

const res = (value?: string) => ({ headers: new Headers(value === undefined ? {} : { 'Retry-After': value }) })

describe('retryAfterSeconds', () => {
  it('parses whole seconds', () => expect(retryAfterSeconds(res('30'))).toBe(30))
  it('is null when missing, zero, or not a number', () => {
    expect(retryAfterSeconds(res())).toBeNull()
    expect(retryAfterSeconds(res('0'))).toBeNull()
    expect(retryAfterSeconds(res('Wed, 21 Oct 2026 07:28:00 GMT'))).toBeNull()
    expect(retryAfterSeconds(undefined)).toBeNull()
  })
  it('caps absurd values', () => expect(retryAfterSeconds(res('999999'))).toBe(3600))
})

describe('rateLimitMessage', () => {
  const t = (key: string, named?: Record<string, unknown>) => `${key}:${named?.seconds ?? ''}`
  it('uses the timed message only with a delay', () => {
    expect(rateLimitMessage(t, 12)).toBe('common.rateLimitedIn:12')
    expect(rateLimitMessage(t, null)).toBe('common.rateLimited:')
  })
})
