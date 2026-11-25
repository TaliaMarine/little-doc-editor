import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import WebSocket from 'ws'
import * as Y from 'yjs'
import { Redis as IORedis } from 'ioredis'
import { HocuspocusProvider, HocuspocusProviderWebsocket } from '@hocuspocus/provider'
import { buildServer } from '../src/app.js'
import { config } from '../src/config.js'
import { PAGE_CONTENT_CHANNEL, replaceLiveContent } from '../src/contentSync.js'
import { docToContent, extractPlainText, replaceFragmentContent } from '../src/convert.js'
import { closePersistence, pool } from '../src/persistence.js'
import { addMember, createPage, createSpace, createUser, doc } from './fixtures.js'

let server: ReturnType<typeof buildServer>
let url: string
let publisher: IORedis
const providers: HocuspocusProvider[] = []

beforeAll(async () => {
  // Redis on (the subscriber needs it); a debounce far longer than any test so
  // an update can only come from the pub/sub path, never from a store.
  server = buildServer({ port: 0, redis: true, debounce: 60_000, maxDebounce: 120_000 })
  await server.listen()
  url = `ws://127.0.0.1:${server.address.port}`
  publisher = new IORedis({ host: config.redis.host, port: config.redis.port, password: config.redis.password })
})
afterAll(async () => {
  providers.forEach(p => p.destroy())
  publisher.disconnect()
  await server.destroy()
  await closePersistence()
})

function connect (name: string, token: string) {
  const ydoc = new Y.Doc()
  const events = { synced: false }
  const socket = new HocuspocusProviderWebsocket({ url, WebSocketPolyfill: WebSocket })
  const provider = new HocuspocusProvider({
    name, token, document: ydoc, websocketProvider: socket,
    onSynced: () => { events.synced = true },
  })
  provider.attach()
  providers.push(provider)
  return { ydoc, events }
}

async function until (cond: () => boolean, ms = 5000) {
  const start = Date.now()
  while (!cond()) {
    if (Date.now() - start > ms) throw new Error('timeout waiting for condition')
    await new Promise(r => setTimeout(r, 25))
  }
}

const text = (ydoc: Y.Doc) => extractPlainText(docToContent(ydoc))
const setContent = (id: string, content: unknown) =>
  pool.query('UPDATE pages SET content = $2::jsonb, updated_at = now() WHERE id = $1', [id, JSON.stringify(content)])

async function setup (initial: string) {
  const space = await createSpace()
  const editor = await createUser()
  await addMember(space, editor.id, 'editor')
  const page = await createPage(space, doc(initial))
  const c = connect(page, editor.token)
  await until(() => c.events.synced)
  expect(text(c.ydoc)).toBe(initial)
  return { page, c }
}

describe('content sync (api restore / PUT -> live room)', () => {
  it('pushes a changed pages.content to a connected provider immediately', async () => {
    const { page, c } = await setup('before restore')
    await setContent(page, doc('restored text'))
    await publisher.publish(PAGE_CONTENT_CHANNEL, page)
    await until(() => text(c.ydoc) === 'restored text')
    // and the snapshot caught up, so a later store won't resurrect old text
    const { rows } = await pool.query('SELECT state FROM collab_documents WHERE page_id = $1', [page])
    const d = new Y.Doc()
    Y.applyUpdate(d, new Uint8Array(rows[0].state))
    expect(text(d)).toBe('restored text')
  })

  it('ignores a message for an unchanged page (editor-save echo)', async () => {
    const { page, c } = await setup('same text')
    await publisher.publish(PAGE_CONTENT_CHANNEL, page)
    await new Promise(r => setTimeout(r, 300))
    expect(text(c.ydoc)).toBe('same text')
    expect(await replaceLiveContent(server.hocuspocus, page)).toBe('unchanged')
  })

  it('is a no-op for pages without a live room, and ignores garbage payloads', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('cold'))
    expect(await replaceLiveContent(server.hocuspocus, page)).toBe('no-room')
    expect(await replaceLiveContent(server.hocuspocus, 'not-a-uuid')).toBe('invalid')
    expect(await replaceLiveContent(server.hocuspocus, "x'; DROP TABLE pages;--")).toBe('invalid')
    await publisher.publish(PAGE_CONTENT_CHANNEL, 'garbage')
    const { rowCount } = await pool.query('SELECT 1 FROM collab_documents WHERE page_id = $1', [page])
    expect(rowCount).toBe(0)
  })

  it('re-baselines the snapshot when pages.content echoes unstored live edits', async () => {
    const { page, c } = await setup('start')
    c.ydoc.transact(() => replaceFragmentContent(c.ydoc, doc('typed live')))
    await until(() => server.hocuspocus.documents.get(page) !== undefined && extractPlainText(docToContent(server.hocuspocus.documents.get(page)!)) === 'typed live')
    await setContent(page, doc('typed live')) // PUT echoing the editor state
    expect(await replaceLiveContent(server.hocuspocus, page)).toBe('unchanged')
    const { rows } = await pool.query('SELECT state FROM collab_documents WHERE page_id = $1', [page])
    const d = new Y.Doc()
    Y.applyUpdate(d, new Uint8Array(rows[0].state))
    expect(text(d)).toBe('typed live')
  })

  it('reports replaced via the direct API too', async () => {
    const { page, c } = await setup('one')
    await setContent(page, doc('two'))
    expect(await replaceLiveContent(server.hocuspocus, page)).toBe('replaced')
    await until(() => text(c.ydoc) === 'two')
  })
})
