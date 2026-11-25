import { beforeEach, describe, expect, it } from 'vitest'
import { clearShare, isValidShareKey, purgeExpiredShares, readShare, SHARE_TTL_MS, stashShare } from '@/utils/shareStash'

function cacheKey (req: RequestInfo | URL) {
  return new URL(typeof req === 'string' ? req : (req instanceof Request ? req.url : req.toString()), 'http://localhost').href
}

// Minimal in-memory Cache API (jsdom has none).
class MemoryCache {
  store = new Map<string, Response>()
  async put (req: RequestInfo | URL, res: Response) {
    this.store.set(cacheKey(req), res.clone())
  }

  async match (req: RequestInfo | URL) {
    return this.store.get(cacheKey(req))?.clone()
  }

  async delete (req: RequestInfo | URL) {
    return this.store.delete(cacheKey(req))
  }

  async keys () {
    return [...this.store.keys()].map(url => new Request(url))
  }
}

beforeEach(() => {
  const cache = new MemoryCache()
  ;(globalThis as unknown as { caches: unknown }).caches = { open: async () => cache }
})

function form (files: File[]) {
  const data = new FormData()
  data.set('title', 'T')
  data.set('text', 'body')
  for (const file of files) {
    data.append('file', file)
  }
  return data
}

describe('shareStash', () => {
  it('stashes files and reads them back with names and types', async () => {
    const key = await stashShare(form([new File(['hello'], 'a b.txt', { type: 'text/plain' }), new File(['x'], 'p.png', { type: 'image/png' })]))
    expect(isValidShareKey(key)).toBe(true)
    const share = await readShare(key)
    expect(share?.title).toBe('T')
    expect(share?.files.map(f => f.name)).toEqual(['a b.txt', 'p.png'])
    expect(await share!.files[0]!.text()).toBe('hello')
  })

  it('clear removes the whole entry', async () => {
    const key = await stashShare(form([new File(['x'], 'a.txt')]))
    await clearShare(key)
    expect(await readShare(key)).toBeNull()
  })

  it('rejects malformed keys', async () => {
    expect(await readShare('../etc')).toBeNull()
    expect(isValidShareKey(null)).toBe(false)
  })

  it('purges abandoned shares after the TTL', async () => {
    const now = 1_000_000
    const key = await stashShare(form([new File(['x'], 'a.txt')]), now)
    await purgeExpiredShares(now + 1000)
    expect(await readShare(key)).not.toBeNull()
    await purgeExpiredShares(now + SHARE_TTL_MS + 1)
    expect(await readShare(key)).toBeNull()
  })
})
