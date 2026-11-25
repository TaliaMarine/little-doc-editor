// One-time stash of shared files (SPEC.md §5.1, Decisions 32-33).
//
// The service worker receives the Android share-target POST, writes the files
// here (Cache API) under a random one-time key, and 303-redirects to
// `/share-target?key=…`; the SPA route reads them back and clears the entry.
// Used from BOTH the service worker and the page, so it only touches APIs
// present in both (Cache API, Request/Response, crypto).
export const SHARE_CACHE = 'lde-share-stash-v1'
// Abandoned shares (user never finished the picker) are purged after this.
export const SHARE_TTL_MS = 24 * 60 * 60 * 1000

const ORIGIN_PREFIX = '/__share-stash/'

export interface ShareMeta {
  title: string
  text: string
  count: number
  createdAt: number
}

export interface SharedPayload extends ShareMeta {
  files: File[]
}

function entryUrl (key: string, name: string): string {
  return `${ORIGIN_PREFIX}${encodeURIComponent(key)}/${name}`
}

// Keys come from the URL, so refuse anything but our own random UUIDs.
export function isValidShareKey (key: string | null | undefined): key is string {
  return typeof key === 'string' && /^[\da-f-]{36}$/i.test(key)
}

export async function stashShare (formData: FormData, now = Date.now()): Promise<string> {
  const cache = await caches.open(SHARE_CACHE)
  await purgeExpiredShares(now)

  const key = crypto.randomUUID()
  const files = formData.getAll('file').filter((value): value is File => typeof value !== 'string')
  const meta: ShareMeta = {
    title: String(formData.get('title') ?? ''),
    text: String(formData.get('text') ?? ''),
    count: files.length,
    createdAt: now,
  }
  // The meta entry is written last: a key whose meta exists is complete.
  for (const [index, file] of files.entries()) {
    await cache.put(entryUrl(key, String(index)), new Response(await file.arrayBuffer(), {
      headers: {
        'Content-Type': file.type || 'application/octet-stream',
        'X-Share-Filename': encodeURIComponent(file.name || `shared-file-${index + 1}`),
      },
    }))
  }
  await cache.put(entryUrl(key, 'meta'), Response.json(meta))
  return key
}

export async function readShare (key: string): Promise<SharedPayload | null> {
  if (!isValidShareKey(key)) {
    return null
  }
  const cache = await caches.open(SHARE_CACHE)
  const metaResponse = await cache.match(entryUrl(key, 'meta'))
  if (!metaResponse) {
    return null
  }
  const meta = await metaResponse.json() as ShareMeta
  const files: File[] = []
  for (let index = 0; index < meta.count; index++) {
    const response = await cache.match(entryUrl(key, String(index)))
    if (!response) {
      continue
    }
    const blob = await response.blob()
    const name = decodeURIComponent(response.headers.get('X-Share-Filename') ?? `shared-file-${index + 1}`)
    files.push(new File([blob], name, { type: blob.type }))
  }
  return { ...meta, files }
}

export async function clearShare (key: string): Promise<void> {
  if (!isValidShareKey(key)) {
    return
  }
  const cache = await caches.open(SHARE_CACHE)
  const prefix = entryUrl(key, '')
  for (const request of await cache.keys()) {
    if (new URL(request.url).pathname.startsWith(prefix)) {
      await cache.delete(request)
    }
  }
}

export async function purgeExpiredShares (now = Date.now()): Promise<void> {
  const cache = await caches.open(SHARE_CACHE)
  for (const request of await cache.keys()) {
    const pathname = new URL(request.url).pathname
    if (!pathname.endsWith('/meta')) {
      continue
    }
    const meta = await (await cache.match(request))?.json() as ShareMeta | undefined
    if (!meta || now - meta.createdAt > SHARE_TTL_MS) {
      const key = decodeURIComponent(pathname.slice(ORIGIN_PREFIX.length, -'/meta'.length))
      await clearShare(key)
    }
  }
}
