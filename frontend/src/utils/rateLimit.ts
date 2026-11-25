// 429 handling shared by the login / invite-redeem / share-redeem forms. The
// api answers 429 with a `Retry-After` header (whole seconds).

// Seconds to wait, or null when the header is missing/garbage. Capped so a
// hostile or misconfigured proxy can't show "try again in 99999999 seconds".
export function retryAfterSeconds (response: Pick<Response, 'headers'> | undefined): number | null {
  const raw = response?.headers?.get('Retry-After')
  if (!raw || !/^\d+$/.test(raw.trim())) {
    return null
  }
  const seconds = Number(raw)
  return seconds >= 1 ? Math.min(seconds, 3600) : null
}

export class RateLimitedError extends Error {
  readonly retryAfter: number | null
  constructor (retryAfter: number | null) {
    super('rate limited')
    this.name = 'RateLimitedError'
    this.retryAfter = retryAfter
  }
}

// User-facing text; `t` is vue-i18n's translate function.
export function rateLimitMessage (t: (key: string, named?: Record<string, unknown>) => string, retryAfter: number | null): string {
  return retryAfter === null ? t('common.rateLimited') : t('common.rateLimitedIn', { seconds: retryAfter })
}
