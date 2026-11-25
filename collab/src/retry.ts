// Exponential backoff for transient failures (Postgres blips during a store).
export async function withRetry<T> (
  fn: () => Promise<T>,
  opts: {
    attempts: number
    baseMs: number
    maxMs?: number
    // Return false for errors that will never succeed on retry.
    retryable?: (err: unknown) => boolean
    onFailure?: (err: unknown, attempt: number) => void
    sleep?: (ms: number) => Promise<void>
  },
): Promise<T> {
  const sleep = opts.sleep ?? (ms => new Promise<void>(resolve => setTimeout(resolve, ms)))
  for (let attempt = 1; ; attempt++) {
    try {
      return await fn()
    } catch (err) {
      opts.onFailure?.(err, attempt)
      if (attempt >= opts.attempts || opts.retryable?.(err) === false) {
        throw err
      }
      await sleep(Math.min(opts.baseMs * 2 ** (attempt - 1), opts.maxMs ?? 30_000))
    }
  }
}
