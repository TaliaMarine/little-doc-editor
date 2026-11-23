// Persists periodic Yjs document-state snapshots to Postgres so the api
// service can read consistent content for non-realtime consumers — search
// indexing, export, revision snapshots (SPEC.md §4). The state is an opaque
// Yjs update blob (Decision 1): this module never interprets it, only
// stores/loads it against the page_id it belongs to.
import { Pool } from 'pg'
import * as Y from 'yjs'
import { config } from './config.js'

// Shared with access.ts's per-document authorization queries.
export const pool = new Pool({ connectionString: config.databaseUrl })

// pg-pool emits 'error' when an *idle* client's connection dies (Postgres
// restart/failover, server-side idle kill). With no listener Node treats it
// as an uncaught exception and the process exits — on every replica at once,
// since they share the database. The pool has already discarded the client;
// logging is all that's needed.
pool.on('error', err => {
  console.error('postgres idle client error (client discarded)', err)
})

// Postgres SQLSTATE for a foreign-key violation: the page row is gone.
const FOREIGN_KEY_VIOLATION = '23503'

export class PageDeletedError extends Error {}

export async function loadDocumentState(pageId: string): Promise<Uint8Array | null> {
  const { rows } = await pool.query<{ state: Buffer }>(
    'SELECT state FROM collab_documents WHERE page_id = $1',
    [pageId],
  )
  return rows[0] ? new Uint8Array(rows[0].state) : null
}

// Merges state into the stored snapshot rather than overwriting it. Each
// replica writes its own full Y.encodeStateAsUpdate, and the Redis
// extension's store lock is only held for ~1s — a slow write from replica A
// could otherwise land after replica B's newer one and silently roll the
// snapshot back. Merging CRDT updates is monotonic, so write order no longer
// matters. (Opaque to api either way — Decision 1.)
//
// Throws PageDeletedError if the page no longer exists.
export async function storeDocumentState(pageId: string, state: Uint8Array): Promise<void> {
  const client = await pool.connect()
  try {
    await client.query('BEGIN')
    // Ensure a row exists so FOR UPDATE has something to lock even on the
    // very first store (two replicas' first stores would otherwise race).
    await client.query(
      `INSERT INTO collab_documents (page_id, state) VALUES ($1, $2)
       ON CONFLICT (page_id) DO NOTHING`,
      [pageId, Buffer.from(state)],
    )
    const { rows } = await client.query<{ state: Buffer }>(
      'SELECT state FROM collab_documents WHERE page_id = $1 FOR UPDATE',
      [pageId],
    )
    const merged = rows[0] ? Y.mergeUpdates([new Uint8Array(rows[0].state), state]) : state
    await client.query(
      'UPDATE collab_documents SET state = $2, updated_at = now() WHERE page_id = $1',
      [pageId, Buffer.from(merged)],
    )
    await client.query('COMMIT')
  } catch (err) {
    await client.query('ROLLBACK').catch(() => {})
    if ((err as { code?: string }).code === FOREIGN_KEY_VIOLATION) {
      throw new PageDeletedError(`page ${pageId} no longer exists`)
    }
    throw err
  } finally {
    client.release()
  }
}

export async function closePersistence(): Promise<void> {
  await pool.end()
}
