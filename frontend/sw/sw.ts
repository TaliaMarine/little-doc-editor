/// <reference lib="webworker" />
// Hand-written service worker (SPEC.md §5.1, Decisions 31-33). Its only jobs:
// make the app installable and catch the Android share-target POST. It does
// NOT cache page content or serve anything offline (Decision 31), and it has
// no precache manifest.
import { purgeExpiredShares, stashShare } from '../src/utils/shareStash'

declare const self: ServiceWorkerGlobalScope

const SHARE_PATH = '/share-target'

self.addEventListener('install', () => {
  // Take over immediately so a share arriving right after install/update
  // isn't sent to a page with no worker.
  self.skipWaiting()
})

self.addEventListener('activate', event => {
  event.waitUntil(Promise.all([self.clients.claim(), purgeExpiredShares().catch(() => {})]))
})

self.addEventListener('fetch', event => {
  const { request } = event
  const url = new URL(request.url)
  if (request.method !== 'POST' || url.origin !== self.location.origin || url.pathname !== SHARE_PATH) {
    return
  }
  event.respondWith(handleShare(request))
})

async function handleShare (request: Request): Promise<Response> {
  const target = new URL(SHARE_PATH, self.location.origin)
  try {
    const key = await stashShare(await request.formData())
    target.searchParams.set('key', key)
  } catch {
    // The SPA route shows a clear "nothing to share" error for this.
    target.searchParams.set('error', '1')
  }
  // 303 so the browser follows with a GET (a SPA route can't read a POST).
  return Response.redirect(target.toString(), 303)
}
