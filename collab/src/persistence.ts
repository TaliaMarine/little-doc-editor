// Persists Yjs document-state snapshots to Postgres (collab_documents) and
// keeps the non-realtime copy of the content current: pages.content
// (ProseMirror JSON) and pages.search_vector, so api's GET /pages/{id},
// search and export stay correct while editing happens in collab (SPEC.md
// §4). The snapshot blob itself stays opaque to api (Decision 1); collab owns
// the Yjs <-> JSON conversion (convert.ts, schema.ts).
import { Pool, type PoolClient } from 'pg'
import * as Y from 'yjs'
import { config } from './config.js'
import { contentToYDoc, extractPlainText, normalizeContent, replaceFragmentContent, sameContent, stateToContent } from './convert.js'
import { externalReplacements, writebackFailures } from './metrics.js'

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

// Who a live write-back is attributed to in page_revisions: the connection's
// user or guest identity (SessionIdentity fits). Both unset = unknown author.
export interface RevisionAuthor {
  userId?: string
  guestIdentityId?: string
}

export class PageDeletedError extends Error {}

// What storeDocumentState tells the caller. `applyToLive`, when set, is a Yjs
// update the live document must apply: a manual content write was merged into
// the snapshot (see below) and the in-memory document has to catch up.
export interface StoreResult {
  applyToLive?: Uint8Array
}

// True when pages.content is not what this collab_documents snapshot would
// have written: i.e. something else (api's manual PUT /pages/{id}/content)
// changed the page since collab last stored. Both sides go through the same
// schema+Yjs round trip so schema normalisation doesn't count as a change.
// Snapshots that can't be converted (unknown node from a newer frontend)
// never count as external — we must not overwrite live state we can't read.
function isExternalEdit(state: Uint8Array, pageContent: unknown): boolean {
  try {
    return !sameContent(stateToContent(state), normalizeContent(pageContent))
  } catch {
    return false
  }
}

// Folds a manual content write into `state` as *real Yjs operations*:
// the old fragment content is deleted and the new content inserted by a fresh
// random client ID. (Re-seeding with the fixed seed client ID would reuse
// clocks with different content and corrupt every peer that still holds the
// old state; and replacing the snapshot instead of merging would resurrect
// the old content on reconnecting peers.) The result is a superset of `state`.
function applyExternalContent(state: Uint8Array, content: unknown): Uint8Array {
  const d = new Y.Doc()
  Y.applyUpdate(d, state)
  try {
    if (sameContent(stateToContent(state), normalizeContent(content))) {
      return state
    }
  } catch {
    // fall through and replace
  }
  replaceFragmentContent(d, content)
  return Y.encodeStateAsUpdate(d)
}

// Returns the Yjs state to load for a page, seeding it from pages.content
// (ProseMirror JSON) when no snapshot exists yet. Seeding happens here, in
// collab, because collab is the only service that knows the Tiptap
// schema/Yjs mapping (Decision 1); doing it on "first client connect" would
// race several clients into duplicate seeds. The first seed uses a fixed Yjs
// client ID, so replicas that both seed produce identical updates that merge
// idempotently, and the row is written immediately so later loads read the
// same bytes.
//
// If a snapshot exists but pages.content was changed manually since (api's
// PUT /content), the manual content is merged in as real edits
// (applyExternalContent) under the same row locks the store path uses.
//
// Returns null if the page no longer exists. Throws if pages.content cannot
// be converted at all: starting blank would let the next store overwrite the
// real content.
export async function loadDocumentState(pageId: string): Promise<Uint8Array | null> {
  const client = await pool.connect()
  try {
    await client.query('BEGIN')
    // Lock order everywhere: pages row, then collab_documents row.
    const { rows } = await client.query<{ content: unknown }>(
      'SELECT content FROM pages WHERE id = $1 FOR UPDATE', [pageId])
    if (!rows[0]) {
      await client.query('ROLLBACK')
      return null
    }
    const content = rows[0].content
    const existing = await client.query<{ state: Buffer }>(
      'SELECT state FROM collab_documents WHERE page_id = $1 FOR UPDATE', [pageId])
    let result: Uint8Array
    if (existing.rows[0]) {
      const state = new Uint8Array(existing.rows[0].state)
      if (!isExternalEdit(state, content)) {
        await client.query('COMMIT')
        return state
      }
      console.warn(`page ${pageId}: pages.content changed outside collab; merging it into the live state`)
      externalReplacements.inc()
      result = applyExternalContent(state, content)
      await client.query('UPDATE collab_documents SET state = $2, updated_at = now() WHERE page_id = $1',
        [pageId, Buffer.from(result)])
    } else {
      result = Y.encodeStateAsUpdate(contentToYDoc(content))
      await client.query('INSERT INTO collab_documents (page_id, state) VALUES ($1, $2)',
        [pageId, Buffer.from(result)])
    }
    await client.query('COMMIT')
    return result
  } catch (err) {
    await client.query('ROLLBACK').catch(() => {})
    throw err
  } finally {
    client.release()
  }
}

interface PageRow {
  content: unknown
  title: string
  history_enabled: boolean | null
  created_by_user_id: string | null
  created_by_guest_id: string | null
}

interface RevisionRow {
  author_user_id: string | null
  author_guest_id: string | null
  same_as_after: boolean
  same_as_before: boolean
  recent: boolean
}

// Mirrors api's revisions.Recorder.OnSave (SPEC.md Decision 42), inside the
// store transaction that already holds the pages row lock (api locks the same
// row first), so a concurrent api save and a collab store can't both record
// or both skip. Skipped when history is off for the page (history_enabled =
// false; NULL records, like api), when content+title equal the latest
// revision, or when the latest revision is by the same author less than the
// debounce ago. A page with no revision yet gets its pre-save state recorded
// first as a baseline (under its creator); a different author saving over
// content the previous author left unrecorded gets that state recorded under
// the previous author first. created_at uses clock_timestamp() (now() is
// constant within a transaction) so these ordered inserts keep their order.
// Author ids are checked for existence (a deleted user must not fail the
// store through the FK); unknown author => NULL/NULL, which never debounces.
async function recordRevision(client: PoolClient, pageId: string, before: PageRow, afterContent: unknown, author: RevisionAuthor): Promise<void> {
  if (before.history_enabled === false) return
  const afterJson = JSON.stringify(afterContent)
  const beforeJson = JSON.stringify(before.content)
  const known = await client.query<{ user_id: string | null, guest_id: string | null }>(
    `SELECT (SELECT id FROM users WHERE id = $1::uuid) AS user_id,
            (SELECT id FROM guest_identities WHERE id = $2::uuid) AS guest_id`,
    [author.userId ?? null, author.guestIdentityId ?? null])
  const a = { userId: known.rows[0].user_id, guestId: known.rows[0].guest_id }
  const insert = (content: string, userId: string | null, guestId: string | null) => client.query(
    `INSERT INTO page_revisions (page_id, content, title, author_user_id, author_guest_id, created_at)
     VALUES ($1, $2::jsonb, $3, $4, $5, clock_timestamp())`,
    [pageId, content, before.title, userId, guestId])

  const { rows } = await client.query<RevisionRow>(
    `SELECT author_user_id, author_guest_id,
            (content = $2::jsonb AND title IS NOT DISTINCT FROM $4) AS same_as_after,
            content = $3::jsonb AS same_as_before,
            clock_timestamp() - created_at < make_interval(secs => $5::float8 / 1000) AS recent
     FROM page_revisions WHERE page_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`,
    [pageId, afterJson, beforeJson, before.title, config.revisionDebounceMs])
  const latest = rows[0]
  if (!latest) {
    // Predates history: keep the pre-save state as the baseline.
    await insert(beforeJson, before.created_by_user_id, before.created_by_guest_id)
    await insert(afterJson, a.userId, a.guestId)
    return
  }
  if (latest.same_as_after) return
  // Same author as api's sameAuthor: a user id on either side decides;
  // otherwise guests compare. Unknown authors never match.
  const same = (latest.author_user_id || a.userId)
    ? latest.author_user_id === a.userId
    : latest.author_guest_id !== null && latest.author_guest_id === a.guestId
  // Deviation from api (which debounces per author only): a live session's
  // author is just whichever connection made the *last* change before the
  // store (updates relayed via Redis carry no context), so with several
  // editors the author flips on nearly every store and the per-author rule
  // would write a full-content revision every 2-10s. Any recent revision
  // therefore debounces a live store, whoever wrote it.
  if (config.revisionDebounceMs > 0 && latest.recent) return
  if (!same && !latest.same_as_before) {
    await insert(beforeJson, latest.author_user_id, latest.author_guest_id)
  }
  await insert(afterJson, a.userId, a.guestId)
}

// Merges the live document into the stored snapshot (rather than overwriting
// it; see below), then writes the derived ProseMirror JSON to pages.content
// and the search vector — all in one transaction, so the snapshot and
// pages.content never disagree about which one is newer.
//
// Each replica writes its own full Y.encodeStateAsUpdate, and the Redis
// extension's store lock is only held for ~1s — a slow write from replica A
// could otherwise land after replica B's newer one and silently roll the
// snapshot back. Merging CRDT updates is monotonic, so write order no longer
// matters.
//
// Manual-write protection: the pages and collab_documents rows are locked
// (same order as load), and if pages.content differs from what the stored
// snapshot represents (api's PUT /content ran since the last store) the
// manual content is merged into the snapshot as real Yjs operations *inside
// this transaction* (applyExternalContent) — so two replicas can't both
// replace it — and returned as `applyToLive` for the caller to apply to the
// in-memory document. Last write wins: edits in the last debounce window that
// the manual content didn't include are overwritten.
//
// pages.updated_at is bumped only when content actually changed; api's own
// PUT does the same.
//
// Throws PageDeletedError if the page no longer exists.
//
// With `onlyExternal` (used by contentSync.ts when api announces a content
// change) nothing is written unless a manual write is detected: no change at
// all rolls back and returns {}.
export async function storeDocumentState(pageId: string, doc: Y.Doc, author: RevisionAuthor = {}, opts: { onlyExternal?: boolean } = {}): Promise<StoreResult> {
  const state = Y.encodeStateAsUpdate(doc)
  const client = await pool.connect()
  try {
    await client.query('BEGIN')
    const page = await client.query<PageRow>(
      `SELECT content, title, history_enabled, created_by_user_id, created_by_guest_id
       FROM pages WHERE id = $1 FOR UPDATE`, [pageId])
    if (!page.rows[0]) {
      throw new PageDeletedError(`page ${pageId} no longer exists`)
    }
    const pageContent = page.rows[0].content
    const ins = await client.query(
      `INSERT INTO collab_documents (page_id, state) VALUES ($1, $2)
       ON CONFLICT (page_id) DO NOTHING`,
      [pageId, Buffer.from(state)],
    )
    const { rows } = await client.query<{ state: Buffer }>(
      'SELECT state FROM collab_documents WHERE page_id = $1 FOR UPDATE', [pageId])
    const stored = new Uint8Array(rows[0].state)
    let merged = ins.rowCount === 0 ? Y.mergeUpdates([stored, state]) : state
    let applyToLive: Uint8Array | undefined
    const external = ins.rowCount === 0 && isExternalEdit(stored, pageContent)
    if (external) {
      // Compare against the merged (live) content: a PUT that merely echoes
      // the editor state needs no replacement.
      const replaced = applyExternalContent(merged, pageContent)
      if (replaced !== merged) {
        console.warn(`page ${pageId}: manual content write detected; replacing live document`)
        externalReplacements.inc()
        merged = replaced
        applyToLive = replaced
      }
    }
    // An echo (pages.content equals the live state, snapshot older) is
    // committed so the snapshot is re-baselined to it.
    if (opts.onlyExternal && !external) {
      await client.query('ROLLBACK')
      return {}
    }
    await client.query(
      'UPDATE collab_documents SET state = $2, updated_at = now() WHERE page_id = $1',
      [pageId, Buffer.from(merged)],
    )
    try {
      const content = stateToContent(merged)
      if (!sameContent(content, pageContent)) {
        // search_vector: identical to api's searchindex.IndexPage
        // (to_tsvector('english', <plain text>)); see convert.extractPlainText.
        await client.query(
          `UPDATE pages SET content = $2::jsonb, search_vector = to_tsvector('english', $3), updated_at = now()
           WHERE id = $1`,
          [pageId, JSON.stringify(content), extractPlainText(content)],
        )
        await recordRevision(client, pageId, page.rows[0], content, author)
      }
    } catch (err) {
      if ((err as { code?: string }).code) throw err // a real SQL error: fail the store
      // Conversion failure only: keep the Yjs snapshot, skip the write-back.
      console.error(`page ${pageId}: cannot derive pages.content from Yjs state`, err)
      writebackFailures.inc()
    }
    await client.query('COMMIT')
    return { applyToLive }
  } catch (err) {
    await client.query('ROLLBACK').catch(() => {})
    if ((err as { code?: string, constraint?: string }).code === FOREIGN_KEY_VIOLATION && !(err as { constraint?: string }).constraint?.startsWith('page_revisions_author')) {
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
