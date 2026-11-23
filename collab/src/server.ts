import {
  Server,
  beforeHandleMessagePayload,
  onAuthenticatePayload,
  onLoadDocumentPayload,
  onStoreDocumentPayload,
} from '@hocuspocus/server'
import { Redis } from '@hocuspocus/extension-redis'
import * as Y from 'yjs'

import { config } from './config.js'
import { AccessDeniedError, authorizeDocument } from './access.js'
import { AuthError, SessionIdentity, verifySessionToken } from './auth.js'
import { closePersistence, loadDocumentState, PageDeletedError, storeDocumentState } from './persistence.js'

// Upper bound on graceful shutdown. destroy() waits for every document to
// unload, and a document whose final store keeps failing never unloads — the
// pod would otherwise hang until SIGKILL.
const SHUTDOWN_TIMEOUT_MS = 20_000

// How stale a connection's last access check may get before the next
// incoming message re-runs it (one small indexed query per connection per
// interval at most).
const ACCESS_RECHECK_MS = 5_000
const lastAccessCheck = new WeakMap<object, number>()

const [redisHost, redisPort] = config.redisAddr.split(':')

const server = new Server<SessionIdentity>({
  port: config.port,
  // We install our own handlers below (so the pg pool gets closed too);
  // Hocuspocus's defaults would process.exit() before that finishes.
  stopOnSignals: false,

  extensions: [
    // Fans out updates across collab replicas and to api (Decision 3, 20).
    // Redis is bring-your-own only (Decision 29) — never a bundled/embedded
    // fallback, even for local dev.
    new Redis({
      host: redisHost,
      port: Number(redisPort ?? 6379),
      options: { password: config.redisPassword },
    }),
  ],

  async onAuthenticate(data: onAuthenticatePayload<SessionIdentity>) {
    try {
      const identity = verifySessionToken(data.token)
      // Authorization, not just authentication: the identity must be
      // allowed to open *this* page, and view-level access gets a
      // read-only connection (Hocuspocus then drops its document updates).
      const access = await authorizeDocument(identity, data.documentName)
      if (access.readOnly) {
        data.connectionConfig.readOnly = true
      }
      // Returned value becomes `context` in every subsequent hook for this
      // connection (e.g. onLoadDocument, onStoreDocument, presence).
      return identity
    } catch (err) {
      if (err instanceof AuthError || err instanceof AccessDeniedError) {
        throw new Error(`unauthorized: ${err.message}`)
      }
      throw err
    }
  },

  // Runs before every incoming message is applied; throwing closes the
  // connection. Keeps an open socket subject to the same rules as a fresh
  // connect: token expiry, link revocation/expiry, user disable, and role
  // changes (a demotion to viewer flips the connection read-only in place).
  async beforeHandleMessage(data: beforeHandleMessagePayload<SessionIdentity>) {
    const identity = data.context
    if (Date.now() >= identity.expiresAt) {
      throw new Error('unauthorized: session token expired')
    }
    const now = Date.now()
    const last = lastAccessCheck.get(data.connection)
    if (last !== undefined && now - last < ACCESS_RECHECK_MS) {
      return
    }
    try {
      const access = await authorizeDocument(identity, data.documentName)
      data.connection.readOnly = access.readOnly
      lastAccessCheck.set(data.connection, now)
    } catch (err) {
      if (err instanceof AuthError || err instanceof AccessDeniedError) {
        throw new Error(`unauthorized: ${err.message}`)
      }
      throw err
    }
  },

  async onLoadDocument(data: onLoadDocumentPayload<SessionIdentity>) {
    const state = await loadDocumentState(data.documentName)
    if (state) {
      Y.applyUpdate(data.document, state)
    }
  },

  // Hocuspocus debounces onStoreDocument on its own (default 2s, max 10s),
  // so snapshots are written on a debounce interval, never every keystroke;
  // documentName here is the page's id. These are Yjs snapshots only —
  // PageRevision creation (SPEC.md §6.7) is not built yet (NEXT.md §7).
  async onStoreDocument(data: onStoreDocumentPayload<SessionIdentity>) {
    const state = Y.encodeStateAsUpdate(data.document)
    try {
      await storeDocumentState(data.documentName, state)
    } catch (err) {
      if (err instanceof PageDeletedError) {
        // Nothing to persist into, ever. Rethrowing would make Hocuspocus
        // keep the document in memory forever ("to avoid data loss"),
        // leaking it and blocking shutdown; instead disconnect its clients
        // and return normally so it unloads.
        console.warn(`dropping document for deleted page ${data.documentName}`)
        data.instance.closeConnections(data.documentName)
        return
      }
      // Transient DB error: Hocuspocus keeps the doc in memory, and the next
      // edit retries the store. (No backoff retry yet — see NEXT.md §3.)
      throw err
    }
  },
})

async function main() {
  await server.listen()
  console.log(`collab listening on :${config.port}`)
}

let shuttingDown = false
async function shutdown() {
  if (shuttingDown) return
  shuttingDown = true
  const timeout = setTimeout(() => {
    console.error(`shutdown did not finish within ${SHUTDOWN_TIMEOUT_MS}ms; exiting`)
    process.exit(1)
  }, SHUTDOWN_TIMEOUT_MS)
  try {
    await server.destroy()
    await closePersistence()
  } finally {
    clearTimeout(timeout)
  }
  process.exit(0)
}

process.on('SIGINT', shutdown)
process.on('SIGTERM', shutdown)
process.on('SIGQUIT', shutdown)

main().catch((err) => {
  console.error('fatal', err)
  process.exit(1)
})
