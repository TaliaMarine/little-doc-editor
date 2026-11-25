import { describe, expect, it, vi } from 'vitest'
import { AwarenessOwnership, pinAwarenessState } from '../src/awareness.js'
import { isOriginAllowed, parseAllowedOrigins } from '../src/origins.js'
import { parseHostPort, parseRedisSettings } from '../src/redisConfig.js'
import { isCanonicalJwt, verifySessionToken } from '../src/auth.js'
import { parseDuration } from '../src/config.js'
import { token } from './fixtures.js'
import { withRetry } from '../src/retry.js'

describe('redis config', () => {
  it('parses host:port, bare host, and IPv6', () => {
    expect(parseHostPort('redis:6380')).toEqual({ host: 'redis', port: 6380 })
    expect(parseHostPort('redis')).toEqual({ host: 'redis', port: 6379 })
    expect(parseHostPort('[::1]:6380')).toEqual({ host: '::1', port: 6380 })
    expect(parseHostPort('[fd00::5]')).toEqual({ host: 'fd00::5', port: 6379 })
    expect(parseHostPort('fd00::5')).toEqual({ host: 'fd00::5', port: 6379 })
    expect(() => parseHostPort('redis:abc')).toThrow()
  })
  it('parses REDIS_URL with TLS, ACL user, password and db', () => {
    expect(parseRedisSettings({ REDIS_URL: 'rediss://app:p%40ss@[::1]:6390/2' })).toEqual({
      host: '::1', port: 6390, username: 'app', password: 'p@ss', db: 2, tls: {},
    })
    const s = parseRedisSettings({ REDIS_URL: 'rediss://cache.example.com' })
    expect(s.tls).toEqual({ servername: 'cache.example.com' })
  })
  it('lets env override credentials and enables TLS for REDIS_ADDR', () => {
    const s = parseRedisSettings({ REDIS_ADDR: 'cache:6379', REDIS_TLS: 'true', REDIS_USERNAME: 'u', REDIS_PASSWORD: 'p' })
    expect(s).toMatchObject({ host: 'cache', username: 'u', password: 'p', tls: { servername: 'cache' } })
  })
  it('requires an address', () => {
    expect(() => parseRedisSettings({})).toThrow()
  })
})

describe('withRetry', () => {
  it('backs off exponentially then succeeds', async () => {
    const sleep = vi.fn(async (_ms: number) => {})
    let n = 0
    const r = await withRetry(async () => { if (++n < 4) throw new Error('x'); return 'ok' }, { attempts: 5, baseMs: 100, sleep })
    expect(r).toBe('ok')
    expect(sleep.mock.calls.map(c => c[0])).toEqual([100, 200, 400])
  })
  it('gives up after attempts and on non-retryable errors', async () => {
    const sleep = vi.fn(async (_ms: number) => {})
    await expect(withRetry(async () => { throw new Error('boom') }, { attempts: 3, baseMs: 1, sleep })).rejects.toThrow('boom')
    expect(sleep).toHaveBeenCalledTimes(2)
    const fn = vi.fn(async () => { throw new Error('perm') })
    await expect(withRetry(fn, { attempts: 5, baseMs: 1, sleep, retryable: () => false })).rejects.toThrow('perm')
    expect(fn).toHaveBeenCalledTimes(1)
  })
})

describe('pinAwarenessState', () => {
  it('pins the name, keeps valid colors, drops unknown keys', () => {
    expect(pinAwarenessState({ user: { name: 'CEO', color: '#aabbcc', x: 1 }, evil: 'y' }, 'Real', false))
      .toEqual({ user: { name: 'Real', color: '#aabbcc' } })
    expect(pinAwarenessState({ user: { name: 'x', color: 'url(javascript:1)' } }, 'Real', false))
      .toEqual({ user: { name: 'Real' } })
  })
  it('keeps cursor for editors only', () => {
    const state = { cursor: { anchor: { type: 1 }, head: { type: 2 }, junk: 1 } }
    expect(pinAwarenessState(state, 'R', false).cursor).toEqual({ anchor: { type: 1 }, head: { type: 2 } })
    expect(pinAwarenessState(state, 'R', true)).toEqual({ user: { name: 'R' } })
  })
})

describe('strict JWT spelling', () => {
  it('accepts the canonical token and rejects re-spelled variants', () => {
    const t = token({ sub: 'u1', displayName: 'U' })
    expect(verifySessionToken(t).userId).toBe('u1')
    const [h, p, sig] = t.split('.')
    // HS256 signatures are 32 bytes = 43 chars; the last char carries 4
    // unused bits, so another char with the same leading bits decodes to the
    // same bytes but spells a different token.
    const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_'
    const idx = alphabet.indexOf(sig[sig.length - 1])
    const alt = alphabet[idx ^ 1]
    const variants = [
      `${h}.${p}.${sig.slice(0, -1)}${alt}`,
      `${h}.${p}.${sig}=`,
      `${h}.${p}\n.${sig}`,
      `${h}.${p}.${sig}.`,
    ]
    // The non-canonical-tail variant is the one a lenient decoder accepts.
    expect(Buffer.from(sig.slice(0, -1) + alt, 'base64url').equals(Buffer.from(sig, 'base64url'))).toBe(true)
    for (const v of variants) {
      expect(isCanonicalJwt(v)).toBe(false)
      expect(() => verifySessionToken(v)).toThrow()
    }
  })
})

describe('duration parsing', () => {
  it('parses Go-style durations', () => {
    expect(parseDuration('X', '5m')).toBe(300_000)
    expect(parseDuration('X', '1h30m')).toBe(5_400_000)
    expect(parseDuration('X', '0')).toBe(0)
    expect(() => parseDuration('X', 'soon')).toThrow()
    expect(() => parseDuration('X', '')).toThrow()
  })
})

describe('origin allow-list', () => {
  it('parses a comma list, normalising case and trailing slashes', () => {
    expect(parseAllowedOrigins(' https://A.example.com/ , ,http://b.example.com:8080')).toEqual(['https://a.example.com', 'http://b.example.com:8080'])
    expect(parseAllowedOrigins(undefined)).toEqual([])
  })
  it('rejects entries that can never match, and drops default ports', () => {
    for (const bad of ['*', 'docs.example.com', 'https://x.com/app', 'ftp://x.com']) expect(() => parseAllowedOrigins(bad)).toThrow()
    expect(parseAllowedOrigins('https://x.com:443')).toEqual(['https://x.com'])
  })
  it('allows everything when unset, otherwise only listed origins (missing Origin passes)', () => {
    expect(isOriginAllowed('https://evil.test', [])).toBe(true)
    const list = ['https://docs.example.com']
    expect(isOriginAllowed('https://DOCS.example.com', list)).toBe(true)
    expect(isOriginAllowed('https://docs.example.com.evil.test', list)).toBe(false)
    expect(isOriginAllowed('null', list)).toBe(false)
    expect(isOriginAllowed(undefined, list)).toBe(true)
  })
})

describe('awareness ownership', () => {
  it('stops a connection touching ids it does not own, and caps claims', () => {
    const own = new AwarenessOwnership<object>()
    const a = {}, b = {}
    const live = () => true
    own.filter(a, new Map([[1, {}]]), new Set(), live)
    const states = new Map<number, unknown>([[1, { x: 1 }], [2, {}], [3, {}], [4, {}]])
    const removed = new Set([1])
    own.filter(b, states, removed, live)
    expect([...states.keys()]).toEqual([2, 3]) // 1 is a's; b claims at most 2
    expect(removed.size).toBe(0)
    // a's own removal passes; a gone => b may take over id 1
    const r = new Set([1]); own.filter(a, new Map(), r, live); expect(r.size).toBe(1)
    const s2 = new Map<number, unknown>([[1, {}]])
    own.filter({}, s2, new Set(), c => c !== a)
    expect(s2.size).toBe(1)
  })
})
