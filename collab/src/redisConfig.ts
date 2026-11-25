// Redis connection settings from the environment, kept free of side effects
// (no env reads at import time) so it can be unit-tested.
//
//   REDIS_URL       redis://[user:pass@]host[:port][/db] or rediss:// (TLS).
//                   Takes precedence over REDIS_ADDR.
//   REDIS_ADDR      host, host:port, [v6]:port or a bare IPv6 literal.
//   REDIS_USERNAME  ACL username (Redis 6+); overrides the URL's.
//   REDIS_PASSWORD  AUTH password; overrides the URL's.
//   REDIS_TLS       "true"/"1" enables TLS for REDIS_ADDR (rediss:// implies it).
import { isIP } from 'node:net'

export interface RedisSettings {
  host: string
  port: number
  username?: string
  password?: string
  db?: number
  // Present when TLS is enabled; servername is set for hostnames so SNI and
  // certificate verification work (never for IP literals).
  tls?: { servername?: string }
}

export function parseHostPort (addr: string): { host: string, port: number } {
  const trimmed = addr.trim()
  if (!trimmed) {
    throw new Error('empty redis address')
  }
  let host: string
  let portStr: string | undefined
  if (trimmed.startsWith('[')) {
    const end = trimmed.indexOf(']')
    if (end < 0) {
      throw new Error(`invalid redis address "${addr}": unterminated [`)
    }
    host = trimmed.slice(1, end)
    const rest = trimmed.slice(end + 1)
    if (rest) {
      if (!rest.startsWith(':')) {
        throw new Error(`invalid redis address "${addr}"`)
      }
      portStr = rest.slice(1)
    }
  } else if (trimmed.split(':').length > 2) {
    // Unbracketed IPv6 literal: no port can be expressed.
    host = trimmed
  } else {
    const idx = trimmed.lastIndexOf(':')
    if (idx < 0) {
      host = trimmed
    } else {
      host = trimmed.slice(0, idx)
      portStr = trimmed.slice(idx + 1)
    }
  }
  if (!host) {
    throw new Error(`invalid redis address "${addr}": empty host`)
  }
  const port = portStr ? Number(portStr) : 6379
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`invalid redis address "${addr}": bad port`)
  }
  return { host, port }
}

function isTrue (v: string | undefined): boolean {
  return v === 'true' || v === '1'
}

export function parseRedisSettings (env: Record<string, string | undefined>): RedisSettings {
  let settings: RedisSettings
  let tls = isTrue(env.REDIS_TLS)
  if (env.REDIS_URL) {
    const url = new URL(env.REDIS_URL)
    if (url.protocol !== 'redis:' && url.protocol !== 'rediss:') {
      throw new Error('REDIS_URL must use redis:// or rediss://')
    }
    tls = tls || url.protocol === 'rediss:'
    // URL.hostname keeps IPv6 brackets.
    const host = url.hostname.replace(/^\[|\]$/g, '')
    settings = {
      host,
      port: url.port ? Number(url.port) : 6379,
      username: url.username ? decodeURIComponent(url.username) : undefined,
      password: url.password ? decodeURIComponent(url.password) : undefined,
      db: url.pathname.length > 1 ? Number(url.pathname.slice(1)) : undefined,
    }
  } else if (env.REDIS_ADDR) {
    settings = parseHostPort(env.REDIS_ADDR)
  } else {
    throw new Error('REDIS_ADDR or REDIS_URL is required')
  }
  if (env.REDIS_USERNAME) settings.username = env.REDIS_USERNAME
  if (env.REDIS_PASSWORD) settings.password = env.REDIS_PASSWORD
  if (tls) {
    settings.tls = isIP(settings.host) ? {} : { servername: settings.host }
  }
  return settings
}
