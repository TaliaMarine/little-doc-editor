import {
  Server,
  beforeHandleAwarenessPayload,
  beforeHandleMessagePayload,
  onAuthenticatePayload,
  onLoadDocumentPayload,
  onRequestPayload,
  onStoreDocumentPayload,
} from '@hocuspocus/server'
import { Redis } from '@hocuspocus/extension-redis'
import { createServer, type Server as HttpServer, type ServerResponse } from 'node:http'
import * as Y from 'yjs'

import { config } from './config.js'
import { AccessDeniedError, authorizeDocument } from './access.js'
import { AwarenessOwnership, pinAwarenessState } from './awareness.js'
import { isOriginAllowed } from './origins.js'
import { AuthError, SessionIdentity, verifySessionToken } from './auth.js'
import {
  accessRevocations, connectionsGauge, documentsGauge, registry, storeFailures, storeLatency,
} from './metrics.js'
import { startContentSync, type ContentSync } from './contentSync.js'
import { loadDocumentState, PageDeletedError, storeDocumentState } from './persistence.js'
import { withRetry } from './retry.js'

// How stale a connection's last access check may get before the next
// incoming message re-runs it (one small indexed query per connection per
// interval at most). The background sweep below uses the same interval so
// idle sockets (a viewer who never sends) are cut off too.
export const ACCESS_RECHECK_MS = Number(process.env.ACCESS_RECHECK_MS ?? 5_000)
const lastAccessCheck = new WeakMap<object, number>()
const ownership = new WeakMap<object, AwarenessOwnership<object>>()

// lastContext is whatever context the last editing connection carried; it can
// be missing/empty (e.g. an update that arrived via Redis from another replica).
function authorOf (ctx: Partial<SessionIdentity> | undefined) {
  return { userId: ctx?.userId, guestIdentityId: ctx?.guestIdentityId }
}

function isAccessError (err: unknown): boolean {
  return err instanceof AuthError || err instanceof AccessDeniedError
}

export interface AppOptions {
  port?: number
  // Redis is bring-your-own and always on in production (Decision 29); tests
  // can opt out to run a single in-process server.
  redis?: boolean
  // Hocuspocus store debounce (ms); defaults are 2s / 10s.
  debounce?: number
  maxDebounce?: number
  // Separate /metrics listener port (0 = serve on the main port).
  metricsPort?: number
  // Overrides COLLAB_ALLOWED_ORIGINS (tests).
  allowedOrigins?: string[]
}

export function buildServer (opts: AppOptions = {}) {
  const redis = config.redis
  const metricsPort = opts.metricsPort ?? config.metricsPort
  const metricsOnMainPort = metricsPort === 0
  const allowedOrigins = opts.allowedOrigins ?? config.allowedOrigins
  const writeMetrics = async (response: ServerResponse) => {
    connectionsGauge.set(server.hocuspocus.getConnectionsCount())
    documentsGauge.set(server.hocuspocus.getDocumentsCount())
    response.writeHead(200, { 'Content-Type': registry.contentType })
    response.end(await registry.metrics())
  }
  const server = new Server<SessionIdentity>({
    port: opts.port ?? config.port,
    quiet: true,
    ...(opts.debounce !== undefined ? { debounce: opts.debounce } : {}),
    ...(opts.maxDebounce !== undefined ? { maxDebounce: opts.maxDebounce } : {}),
    // We install our own handlers (see server.ts) so the pg pool gets closed
    // too; Hocuspocus's defaults would process.exit() before that finishes.
    stopOnSignals: false,

    extensions: opts.redis === false ? [] : [
      // Fans out updates across collab replicas and to api (Decision 3, 20).
      // Redis is bring-your-own only (Decision 29) — never a bundled/embedded
      // fallback, even for local dev.
      new Redis({
        host: redis.host,
        port: redis.port,
        options: {
          username: redis.username,
          password: redis.password,
          db: redis.db,
          tls: redis.tls,
        },
      }),
    ],

    // Refuse a browser handshake from an origin outside COLLAB_ALLOWED_ORIGINS
    // (origins.ts). Rejecting with nothing tells Hocuspocus not to upgrade.
    async onUpgrade ({ request, socket }) {
      if (isOriginAllowed(request.headers.origin, allowedOrigins)) return
      console.warn(`refused WebSocket handshake from origin ${JSON.stringify(String(request.headers.origin).slice(0, 200))} (not in COLLAB_ALLOWED_ORIGINS)`)
      socket.once('finish', () => socket.destroy())
      socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n')
      // Must only ever reject with a falsy value: a real error would be
      // rethrown by Hocuspocus inside an event listener and crash the process.
      return Promise.reject()
    },

    // GET /metrics (Prometheus). Everything else gets Hocuspocus's default
    // response. Unauthenticated like most exporters: expose it on the
    // cluster-internal Service/ServiceMonitor only, never via the public ingress.
    async onRequest ({ request, response }: onRequestPayload) {
      // With METRICS_PORT set, /metrics lives only on that listener.
      if (!metricsOnMainPort) return
      if (request.method === 'GET' && request.url?.split('?')[0] === '/metrics') {
        // Requests that came through an ingress/proxy carry forwarding
        // headers; metrics are for in-cluster scrapers only.
        if (request.headers['x-forwarded-for'] || request.headers['x-forwarded-host'] || request.headers.forwarded) {
          response.writeHead(404)
          response.end()
          return Promise.reject()
        }
        await writeMetrics(response)
        // A falsy rejection tells Hocuspocus the response is already sent.
        return Promise.reject()
      }
    },

    async onAuthenticate (data: onAuthenticatePayload<SessionIdentity>) {
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
        if (isAccessError(err)) {
          throw new Error(`unauthorized: ${(err as Error).message}`)
        }
        throw err
      }
    },

    // Runs before every incoming message is applied; throwing closes the
    // connection. Keeps an open socket subject to the same rules as a fresh
    // connect: token expiry, link revocation/expiry, user disable, and role
    // changes (a demotion to viewer flips the connection read-only in place).
    async beforeHandleMessage (data: beforeHandleMessagePayload<SessionIdentity>) {
      const identity = data.context
      if (Date.now() >= identity.expiresAt) {
        accessRevocations.inc()
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
        if (isAccessError(err)) {
          accessRevocations.inc()
          throw new Error(`unauthorized: ${(err as Error).message}`)
        }
        throw err
      }
    },

    // Presence spoofing guard — see awareness.ts for the policy.
    async beforeHandleAwareness (data: beforeHandleAwarenessPayload<SessionIdentity>) {
      const identity = data.context
      if (!identity) return
      const readOnly = data.connection?.readOnly ?? false
      if (data.connection) {
        let own = ownership.get(data.document)
        if (!own) ownership.set(data.document, own = new AwarenessOwnership())
        own.filter(data.connection, data.states, data.removedClients, c => data.document.connections.has(c as any))
      }
      for (const [clientId, state] of data.states) {
        data.states.set(clientId, pinAwarenessState(state, identity.displayName, readOnly))
      }
    },

    async onLoadDocument (data: onLoadDocumentPayload<SessionIdentity>) {
      const state = await loadDocumentState(data.documentName)
      if (state) {
        Y.applyUpdate(data.document, state)
      }
    },

    // Hocuspocus debounces onStoreDocument on its own (default 2s, max 10s),
    // so snapshots are written on a debounce interval, never every keystroke;
    // documentName here is the page's id. Persists the Yjs snapshot and the
    // derived pages.content/search_vector and, per the Decision 42 rule, a
    // page_revisions row (persistence.ts). The author is the connection that
    // made the last change (lastContext), when known.
    async onStoreDocument (data: onStoreDocumentPayload<SessionIdentity>) {
      try {
        await withRetry(async () => {
          const end = storeLatency.startTimer()
          const result = await storeDocumentState(data.documentName, data.document, authorOf(data.lastContext))
          // A manual PUT /content was merged into the snapshot (last write
          // wins); catch the in-memory document up so clients see it.
          if (result.applyToLive) {
            Y.applyUpdate(data.document, result.applyToLive, 'external-content')
          }
          end()
        }, {
          attempts: config.storeMaxAttempts,
          baseMs: config.storeRetryBaseMs,
          retryable: err => !(err instanceof PageDeletedError),
          onFailure: (err, attempt) => {
            storeFailures.inc()
            console.error(`store of ${data.documentName} failed (attempt ${attempt}/${config.storeMaxAttempts})`, err)
          },
        })
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
        // Retries exhausted: Hocuspocus keeps the doc in memory, and the next
        // edit (or the shutdown flush) tries again.
        throw err
      }
    },
  })

  // Background re-check of every open connection so idle sockets are also cut
  // off on revocation (beforeHandleMessage only fires when a client sends).
  let sweeping = false
  const sweep = setInterval(async () => {
    if (sweeping) return
    sweeping = true
    try {
    for (const doc of [...server.hocuspocus.documents.values()]) {
      for (const connection of [...doc.connections.keys()]) {
        const identity = connection.context as SessionIdentity | undefined
        if (!identity) continue
        try {
          if (Date.now() >= identity.expiresAt) throw new AuthError('session token expired')
          const access = await authorizeDocument(identity, doc.name)
          connection.readOnly = access.readOnly
        } catch (err) {
          if (isAccessError(err)) {
            accessRevocations.inc()
            connection.close({ code: 4401, reason: 'unauthorized' })
          } else {
            console.error('access sweep error', err)
          }
        }
      }
    }
    } finally {
      sweeping = false
    }
  }, ACCESS_RECHECK_MS)
  sweep.unref()
  // Optional metrics-only listener: nothing else is routed here, so a public
  // ingress that only targets the main port can never reach /metrics.
  let metricsServer: HttpServer | undefined
  const listen = server.listen.bind(server)
  server.listen = async (...args: Parameters<typeof listen>) => {
    const res = await listen(...args)
    if (!metricsOnMainPort && !metricsServer) {
      const ms = createServer((req, res) => {
        if (req.method === 'GET' && req.url?.split('?')[0] === '/metrics') {
          writeMetrics(res).catch(() => { res.destroy() })
        } else {
          res.writeHead(404).end()
        }
      })
      await new Promise<void>((resolve, reject) => {
        ms.once('error', reject)
        ms.listen(metricsPort, () => resolve())
      })
      metricsServer = ms
    }
    return res
  }
  // api announces restore / PUT /content on a Redis channel (contentSync.ts).
  let contentSync: ContentSync | undefined
  const listenWithSync = server.listen.bind(server)
  server.listen = async (...args: Parameters<typeof listenWithSync>) => {
    const res = await listenWithSync(...args)
    if (opts.redis !== false && !contentSync) {
      contentSync = startContentSync(server.hocuspocus, redis)
    }
    return res
  }
  const destroy = server.destroy.bind(server)
  server.destroy = async () => {
    clearInterval(sweep)
    await contentSync?.close()
    contentSync = undefined
    if (metricsServer) {
      const ms = metricsServer
      metricsServer = undefined
      await new Promise<void>(resolve => { ms.close(() => resolve()); ms.closeAllConnections() })
    }
    return destroy()
  }

  return server
}
