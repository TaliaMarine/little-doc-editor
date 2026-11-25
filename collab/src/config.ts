import { parseAllowedOrigins } from './origins.js'
import { parseRedisSettings } from './redisConfig.js'

function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} is required`)
  }
  return value
}

// Go-style duration subset (what api's HISTORY_REVISION_DEBOUNCE accepts in
// practice): one or more <number><unit> pairs, e.g. 5m, 90s, 1h30m, 0.
export function parseDuration(name: string, raw: string): number {
  const units: Record<string, number> = { ns: 1e-6, us: 1e-3, 'µs': 1e-3, 'μs': 1e-3, ms: 1, s: 1000, m: 60_000, h: 3_600_000 }
  if (raw === '0') return 0
  const re = /(\d+\.?\d*|\.\d+)(ns|us|µs|μs|ms|s|m|h)/y
  let total = 0
  let pos = 0
  while (pos < raw.length) {
    re.lastIndex = pos
    const m = re.exec(raw)
    if (!m) throw new Error(`${name} must be a duration (e.g. 5m): ${raw}`)
    total += Number(m[1]) * units[m[2]]
    pos = re.lastIndex
  }
  if (pos === 0) throw new Error(`${name} must be a duration (e.g. 5m): ${raw}`)
  return total
}

export const config = {
  port: Number(process.env.PORT ?? 1234),
  // Optional separate listener for GET /metrics only (like api's
  // METRICS_ADDR). 0/unset = metrics stay on the main port (behind the
  // forwarding-header check). When set, /metrics is NOT served on the main
  // port at all.
  metricsPort: Number(process.env.METRICS_PORT ?? 0),
  databaseUrl: required('DATABASE_URL'),
  // REDIS_URL or REDIS_ADDR (+ REDIS_USERNAME / REDIS_PASSWORD / REDIS_TLS);
  // see redisConfig.ts.
  redis: parseRedisSettings(process.env),
  sessionJwtSecret: required('SESSION_JWT_SECRET'),
  // Browser origins allowed to open a WebSocket (see origins.ts); empty = any.
  allowedOrigins: parseAllowedOrigins(process.env.COLLAB_ALLOWED_ORIGINS),
  // Store retry (onStoreDocument): attempts and base delay of the
  // exponential backoff (delay doubles each attempt, capped at 30s).
  storeMaxAttempts: Number(process.env.STORE_MAX_ATTEMPTS ?? 6),
  // Same rule and default as api's HISTORY_REVISION_DEBOUNCE (ms; 0 = off).
  revisionDebounceMs: parseDuration('HISTORY_REVISION_DEBOUNCE', process.env.HISTORY_REVISION_DEBOUNCE || '5m'),
  storeRetryBaseMs: Number(process.env.STORE_RETRY_BASE_MS ?? 500),
}
