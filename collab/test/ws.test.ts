import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import WebSocket from 'ws'
import * as Y from 'yjs'
import { HocuspocusProvider, HocuspocusProviderWebsocket } from '@hocuspocus/provider'
import { prosemirrorJSONToYXmlFragment } from '@tiptap/y-tiptap'
import { buildServer } from '../src/app.js'
import { docToContent, extractPlainText, replaceFragmentContent } from '../src/convert.js'
import { closePersistence, pool } from '../src/persistence.js'
import { schema } from '../src/schema.js'
import { addMember, createPage, createShareLink, createSpace, createUser, doc } from './fixtures.js'

let server: ReturnType<typeof buildServer>
let url: string
const providers: HocuspocusProvider[] = []

beforeAll(async () => {
  // Redis off: a single in-process server; Redis fan-out is Hocuspocus's own.
  server = buildServer({ port: 0, redis: false, debounce: 50, maxDebounce: 200 })
  await server.listen()
  url = `ws://127.0.0.1:${server.address.port}`
})
afterAll(async () => {
  providers.forEach(p => p.destroy())
  await server.destroy()
  await closePersistence()
})

function connect (name: string, token: string) {
  const ydoc = new Y.Doc()
  const events = { authFailed: null as string | null, closed: false, synced: false }
  const socket = new HocuspocusProviderWebsocket({ url, WebSocketPolyfill: WebSocket })
  const provider = new HocuspocusProvider({
    name, token, document: ydoc, websocketProvider: socket,
    onAuthenticationFailed: ({ reason }) => { events.authFailed = reason },
    onSynced: () => { events.synced = true },
    onClose: () => { events.closed = true },
  })
  provider.attach()
  providers.push(provider)
  return { provider, ydoc, events }
}

async function until (cond: () => boolean | Promise<boolean>, ms = 8000) {
  const start = Date.now()
  while (!(await cond())) {
    if (Date.now() - start > ms) throw new Error('timeout waiting for condition')
    await new Promise(r => setTimeout(r, 25))
  }
}

const text = (ydoc: Y.Doc) => extractPlainText(docToContent(ydoc))
const dbContent = async (id: string) => extractPlainText((await pool.query('SELECT content FROM pages WHERE id = $1', [id])).rows[0].content)

async function setup () {
  const space = await createSpace()
  const editor = await createUser()
  const viewer = await createUser()
  const outsider = await createUser()
  await addMember(space, editor.id, 'editor')
  await addMember(space, viewer.id, 'viewer')
  const page = await createPage(space, doc('seeded text'))
  return { space, editor, viewer, outsider, page }
}

describe('collab websocket server', () => {
  it('lets an editor connect, receive seeded content, edit, and persists content + search vector', async () => {
    const { editor, page } = await setup()
    const c = connect(page, editor.token)
    await until(() => c.events.synced)
    expect(text(c.ydoc)).toBe('seeded text')
    replaceFragmentContent(c.ydoc, doc('edited live'))
    await until(async () => (await dbContent(page)) === 'edited live')
    const { rows } = await pool.query("SELECT search_vector @@ to_tsquery('english', 'edited') AS hit FROM pages WHERE id = $1", [page])
    expect(rows[0].hit).toBe(true)
  })

  it('denies users without access and invalid tokens', async () => {
    const { outsider, page } = await setup()
    const denied = connect(page, outsider.token)
    await until(() => denied.events.authFailed !== null)
    expect(denied.events.authFailed).toBe('permission-denied')
    const bad = connect(page, 'garbage')
    await until(() => bad.events.authFailed !== null)
    const wrongName = connect(page.toUpperCase(), (await setup()).editor.token)
    await until(() => wrongName.events.authFailed !== null)
  })

  it('gives viewers a read-only connection whose edits are ignored', async () => {
    const { editor, viewer, page } = await setup()
    const e = connect(page, editor.token)
    const v = connect(page, viewer.token)
    await until(() => e.events.synced && v.events.synced)
    expect(v.provider.authorizedScope).toBe('readonly')
    expect(e.provider.authorizedScope).toBe('read-write')
    replaceFragmentContent(v.ydoc, doc('viewer vandalism'))
    await new Promise(r => setTimeout(r, 400))
    expect(text(e.ydoc)).toBe('seeded text')
    expect(await dbContent(page)).toBe('seeded text')
  })

  it('a manual pages.content write replaces live state at the next store', async () => {
    const { editor, page } = await setup()
    const c = connect(page, editor.token)
    await until(() => c.events.synced)
    replaceFragmentContent(c.ydoc, doc('live one'))
    await until(async () => (await dbContent(page)) === 'live one')
    await pool.query('UPDATE pages SET content = $2::jsonb, updated_at = now() WHERE id = $1', [page, JSON.stringify(doc('manual put'))])
    replaceFragmentContent(c.ydoc, doc('live two')) // triggers the next store
    await until(() => text(c.ydoc) === 'manual put')
    expect(await dbContent(page)).toBe('manual put')
  })

  it('closes sockets when access is revoked, even if the client is idle', async () => {
    const { editor, space, page } = await setup()
    const c = connect(page, editor.token)
    await until(() => c.events.synced)
    c.events.closed = false
    await pool.query('DELETE FROM space_members WHERE space_id = $1 AND user_id = $2', [space, editor.id])
    await until(() => c.events.closed)
    // ...and a fresh connection is refused.
    const again = connect(page, editor.token)
    await until(() => again.events.authFailed !== null)
  })

  it('closes a share-link guest when the link is revoked', async () => {
    const { editor, page } = await setup()
    const link = await createShareLink(page, editor.id, 'edit')
    const c = connect(page, link.token)
    await until(() => c.events.synced)
    expect(c.provider.authorizedScope).toBe('read-write')
    c.events.closed = false
    await pool.query('UPDATE share_links SET revoked_at = now() WHERE id = $1', [link.id])
    await until(() => c.events.closed)
  })

  it('closes connections of a deleted page', async () => {
    const { editor, page } = await setup()
    const c = connect(page, editor.token)
    await until(() => c.events.synced)
    c.events.closed = false
    await pool.query('DELETE FROM pages WHERE id = $1', [page])
    prosemirrorJSONToYXmlFragment(schema, doc('into the void'), c.ydoc.getXmlFragment('default'))
    await until(() => c.events.closed)
  })

  it('pins awareness names server-side', async () => {
    const { editor, viewer, page } = await setup()
    const e = connect(page, editor.token)
    const v = connect(page, viewer.token)
    await until(() => e.events.synced && v.events.synced)
    v.provider.setAwarenessField('user', { name: 'The CEO', color: '#123456' })
    v.provider.setAwarenessField('junk', { big: 'x'.repeat(100) })
    await until(() => [...e.provider.awareness!.getStates().values()].some(s => s.user?.name === viewer.displayName))
    const seen = [...e.provider.awareness!.getStates().entries()].find(([id]) => id === v.provider.awareness!.clientID)![1]
    expect(seen.user).toEqual({ name: viewer.displayName, color: '#123456' })
    expect(seen.junk).toBeUndefined()
  })

  it('attributes a share-link guest edit and awareness name from the token', async () => {
    const { editor, page } = await setup()
    const link = await createShareLink(page, editor.id, 'edit')
    const e = connect(page, editor.token)
    const g = connect(page, link.token)
    await until(() => e.events.synced && g.events.synced)
    g.provider.setAwarenessField('user', { name: 'The CEO', color: '#123456' })
    await until(() => [...e.provider.awareness!.getStates().values()].some(s => s.user?.name === 'Guest G'))
    const seen = e.provider.awareness!.getStates().get(g.provider.awareness!.clientID)!
    expect(seen.user.name).toBe('Guest G')
    replaceFragmentContent(g.ydoc, doc('guest wrote this'))
    await until(async () => (await dbContent(page)) === 'guest wrote this')
    await until(async () => (await pool.query(
      'SELECT 1 FROM page_revisions WHERE page_id = $1 AND author_guest_id = $2', [page, link.guestId])).rowCount === 1)
  })

  it('refuses a WebSocket handshake from an origin outside COLLAB_ALLOWED_ORIGINS', async () => {
    const s = buildServer({ port: 0, redis: false, allowedOrigins: ['https://docs.example.com'] })
    await s.listen()
    const open = (origin?: string) => new Promise<string>((resolve) => {
      const ws = new WebSocket(`ws://127.0.0.1:${s.address.port}`, origin ? { headers: { Origin: origin } } : {})
      ws.on('open', () => { ws.close(); resolve('open') })
      ws.on('unexpected-response', (_req, res) => resolve(String(res.statusCode)))
      ws.on('error', () => resolve('error'))
    })
    try {
      expect(await open('https://evil.example.net')).toBe('403')
      expect(await open('https://docs.example.com')).toBe('open')
      expect(await open()).toBe('open') // not a browser: token auth decides
    } finally {
      await s.destroy()
    }
  })

  it('serves Prometheus metrics on the HTTP port', async () => {
    const res = await fetch(`http://127.0.0.1:${server.address.port}/metrics`)
    expect(res.status).toBe(200)
    const body = await res.text()
    for (const name of ['collab_connections', 'collab_documents', 'collab_store_failures_total', 'collab_store_duration_seconds']) {
      expect(body).toContain(name)
    }
  })

  it('serves /metrics only on METRICS_PORT when it is set', async () => {
    const s = buildServer({ port: 0, redis: false, metricsPort: 39871 })
    await s.listen()
    try {
      expect((await fetch('http://127.0.0.1:39871/metrics')).status).toBe(200)
      expect((await fetch('http://127.0.0.1:39871/other')).status).toBe(404)
      const main = await fetch(`http://127.0.0.1:${s.address.port}/metrics`)
      expect(await main.text()).not.toContain('collab_connections')
    } finally {
      await s.destroy()
    }
  })
})
