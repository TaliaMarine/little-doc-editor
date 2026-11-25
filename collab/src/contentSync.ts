// Immediate merge of content changed outside collab into a live room.
//
// api's POST /pages/{id}/revisions/{rev}/restore and PUT /pages/{id}/content
// change pages.content without touching the Yjs room. collab would only
// notice at its next store (persistence.ts, "manual-write protection"), so
// everyone connected would keep seeing the old text — and their next edit
// would overwrite the change. api therefore publishes the page id on
// PAGE_CONTENT_CHANNEL after committing, and every replica that holds a live
// room for that page runs the same external-content merge right away and
// broadcasts it (Hocuspocus relays document updates to clients and, via the
// Redis extension, to other replicas). No live room here = no-op: the next
// load merges it. A missed message (Redis down) only delays to the next store.
import { Redis as IORedis } from 'ioredis'
import * as Y from 'yjs'
import type { Hocuspocus } from '@hocuspocus/server'
import { PageDeletedError, storeDocumentState } from './persistence.js'
import type { RedisSettings } from './redisConfig.js'

// Payload: the page id (UUID string), nothing else. Keep in sync with
// api/internal/pagesync.Channel.
export const PAGE_CONTENT_CHANNEL = 'lde:page-content-replaced'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export type ExternalSyncResult = 'no-room' | 'unchanged' | 'replaced' | 'invalid' | 'failed'

// Merges pages.content into the live room for `payload` (a page id), if this
// replica has one. Never throws.
export async function replaceLiveContent (hocuspocus: Pick<Hocuspocus, 'documents' | 'loadingDocuments'>, payload: string): Promise<ExternalSyncResult> {
  const pageId = payload.trim()
  if (!UUID_RE.test(pageId)) {
    console.warn('content sync: ignoring message with invalid page id')
    return 'invalid'
  }
  // A room still loading read pages.content before this change was
  // announced; wait for it so the merge below sees it.
  try { await hocuspocus.loadingDocuments.get(pageId) } catch { /* load failed: no room */ }
  const document = hocuspocus.documents.get(pageId)
  if (!document) return 'no-room'
  try {
    const result = await storeDocumentState(pageId, document, {}, { onlyExternal: true })
    if (!result.applyToLive) return 'unchanged'
    Y.applyUpdate(document, result.applyToLive, 'external-content')
    return 'replaced'
  } catch (err) {
    // A deleted page is dropped by the regular store path.
    if (!(err instanceof PageDeletedError)) {
      console.error(`content sync for page ${pageId} failed; it will be merged at the next store`, err)
    }
    return 'failed'
  }
}

// Per page: one run in flight plus at most one rerun queued. The operation is
// idempotent, so a burst of messages collapses without losing the last one
// and cannot pile up pg connections.
const inFlight = new Map<string, { rerun: boolean }>()

export async function replaceLiveContentCoalesced (hocuspocus: Pick<Hocuspocus, 'documents' | 'loadingDocuments'>, payload: string): Promise<void> {
  const key = payload.trim().toLowerCase()
  const running = inFlight.get(key)
  if (running) {
    running.rerun = true
    return
  }
  const state = { rerun: false }
  inFlight.set(key, state)
  try {
    do {
      state.rerun = false
      await replaceLiveContent(hocuspocus, payload)
    } while (state.rerun)
  } finally {
    inFlight.delete(key)
  }
}

export interface ContentSync {
  close (): Promise<void>
}

// Subscribes (own connection: a subscribing ioredis client can't run other
// commands) and dispatches messages to replaceLiveContent. ioredis
// reconnects and resubscribes on its own.
export function startContentSync (hocuspocus: Pick<Hocuspocus, 'documents' | 'loadingDocuments'>, redis: RedisSettings): ContentSync {
  const sub = new IORedis({
    host: redis.host,
    port: redis.port,
    username: redis.username,
    password: redis.password,
    db: redis.db,
    tls: redis.tls,
  })
  sub.on('error', err => console.error('content sync redis error', err))
  sub.subscribe(PAGE_CONTENT_CHANNEL).catch(err => console.error('content sync subscribe failed', err))
  sub.on('message', (channel: string, message: string) => {
    if (channel === PAGE_CONTENT_CHANNEL) void replaceLiveContentCoalesced(hocuspocus, message)
  })
  return {
    async close () {
      sub.removeAllListeners('message')
      sub.disconnect()
    },
  }
}
