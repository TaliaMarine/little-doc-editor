import { afterAll, describe, expect, it } from 'vitest'
import * as Y from 'yjs'
import { contentToYDoc, docToContent, extractPlainText, replaceFragmentContent } from '../src/convert.js'
import { closePersistence, loadDocumentState, PageDeletedError, pool, storeDocumentState } from '../src/persistence.js'
import { createPage, createShareLink, createSpace, createUser, doc } from './fixtures.js'

afterAll(closePersistence)

async function loadDoc (pageId: string) {
  const d = new Y.Doc()
  const state = await loadDocumentState(pageId)
  if (state) Y.applyUpdate(d, state)
  return d
}
const pageRow = async (id: string) => (await pool.query('SELECT content, search_vector::text AS sv, updated_at FROM pages WHERE id = $1', [id])).rows[0]

describe('persistence', () => {
  it('seeds a Yjs doc from pages.content on first load and persists the seed', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('hello world'))
    const d = await loadDoc(page)
    expect(extractPlainText(docToContent(d))).toBe('hello world')
    const { rows } = await pool.query('SELECT 1 FROM collab_documents WHERE page_id = $1', [page])
    expect(rows).toHaveLength(1)
    // A second load returns the same state, not a duplicate seed.
    expect(extractPlainText(docToContent(await loadDoc(page)))).toBe('hello world')
  })

  it('concurrent first loads do not duplicate content', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('once'))
    const [a, b] = await Promise.all([loadDocumentState(page), loadDocumentState(page)])
    const merged = new Y.Doc()
    Y.applyUpdate(merged, a!)
    Y.applyUpdate(merged, b!)
    expect(extractPlainText(docToContent(merged))).toBe('once')
  })

  it('writes live content and the search vector back on store, leaving the title alone', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('before'))
    const d = await loadDoc(page)
    replaceFragmentContent(d, doc('zebra crossing'))
    expect(await storeDocumentState(page, d)).toEqual({})
    const row = await pageRow(page)
    expect(extractPlainText(row.content)).toBe('zebra crossing')
    expect(row.sv).toContain("'zebra'")
    expect(row.sv).not.toContain("'befor'")
  })

  it('does not bump updated_at when content is unchanged', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('same'))
    const d = await loadDoc(page)
    await storeDocumentState(page, d)
    const first = await pageRow(page)
    await storeDocumentState(page, d)
    expect((await pageRow(page)).updated_at).toEqual(first.updated_at)
  })

  it('detects a manual content write and lets it win over the stale snapshot', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('live'))
    const d = await loadDoc(page)
    await storeDocumentState(page, d)
    await pool.query('UPDATE pages SET content = $2::jsonb, updated_at = now() WHERE id = $1', [page, JSON.stringify(doc('manual'))])
    const res = await storeDocumentState(page, d)
    expect(res.applyToLive).toBeDefined()
    expect(extractPlainText((await pageRow(page)).content)).toBe('manual')
    Y.applyUpdate(d, res.applyToLive!)
    expect(extractPlainText(docToContent(d))).toBe('manual')
    expect(extractPlainText(docToContent(await loadDoc(page)))).toBe('manual')
  })

  it('a rename (updated_at bump without content change) is not an external edit', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('x'))
    const d = await loadDoc(page)
    replaceFragmentContent(d, doc('typed live'))
    await pool.query("UPDATE pages SET title = 'Renamed', updated_at = now() WHERE id = $1", [page])
    // pages.content ('x') still equals the snapshot's content: live edits survive.
    expect(await storeDocumentState(page, d)).toEqual({})
    expect(extractPlainText((await pageRow(page)).content)).toBe('typed live')
  })

  it('re-seeds on load when content was edited manually while no live doc existed', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('old'))
    await storeDocumentState(page, await loadDoc(page))
    await pool.query('UPDATE pages SET content = $2::jsonb WHERE id = $1', [page, JSON.stringify(doc('new'))])
    const fresh = await loadDoc(page)
    expect(extractPlainText(docToContent(fresh))).toBe('new')
    // A peer still holding the pre-edit state converges instead of corrupting.
    const peer = await (async () => { const p = contentToYDoc(doc('old')); return p })()
    Y.applyUpdate(peer, Y.encodeStateAsUpdate(fresh))
    Y.applyUpdate(fresh, Y.encodeStateAsUpdate(peer))
    expect(extractPlainText(docToContent(peer))).toBe('new')
    expect(extractPlainText(docToContent(fresh))).toBe('new')
  })

  it('throws PageDeletedError for a deleted page and null on load', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('gone'))
    const d = await loadDoc(page)
    await pool.query('DELETE FROM pages WHERE id = $1', [page])
    await expect(storeDocumentState(page, d)).rejects.toBeInstanceOf(PageDeletedError)
    expect(await loadDocumentState(page)).toBeNull()
  })

  it('keeps the snapshot (and skips write-back) when state cannot be converted', async () => {
    const space = await createSpace()
    const page = await createPage(space, doc('keep'))
    const d = new Y.Doc()
    const frag = d.getXmlFragment('default')
    frag.insert(0, [new Y.XmlElement('notInSchema')])
    expect(await storeDocumentState(page, d)).toEqual({})
    expect(extractPlainText((await pageRow(page)).content)).toBe('keep')
    void contentToYDoc
  })

  describe('revisions (SPEC Decision 42)', () => {
    const revs = async (page: string) => (await pool.query(
      'SELECT content, title, author_user_id, author_guest_id FROM page_revisions WHERE page_id = $1 ORDER BY created_at, id', [page])).rows
    const edit = async (page: string, text: string, author: { userId?: string, guestIdentityId?: string } = {}) => {
      const d = await loadDoc(page)
      replaceFragmentContent(d, doc(text))
      await storeDocumentState(page, d, author)
    }
    const backdate = (page: string) => pool.query("UPDATE page_revisions SET created_at = created_at - interval '10 minutes' WHERE page_id = $1", [page])

    it('records a baseline of the pre-save state, then the new state, for a page without history', async () => {
      const space = await createSpace()
      const u = await createUser()
      const page = await createPage(space, doc('old'))
      await edit(page, 'new', { userId: u.id })
      const r = await revs(page)
      expect(r.map(x => extractPlainText(x.content))).toEqual(['old', 'new'])
      expect(r[0].title).toBe('Test')
      expect(r[0].author_user_id).toBeNull()
      expect(r[1].author_user_id).toBe(u.id)
    })

    it('alternating authors inside the window do not create a revision per store', async () => {
      const space = await createSpace()
      const [u1, u2] = [await createUser(), await createUser()]
      const page = await createPage(space, doc('a'))
      await edit(page, 'b', { userId: u1.id })
      await edit(page, 'c', { userId: u2.id })
      await edit(page, 'd', { userId: u1.id })
      expect(await revs(page)).toHaveLength(2)
    })

    it('skips a same-author save inside the debounce window, and records it once the window passes', async () => {
      const space = await createSpace()
      const u = await createUser()
      const page = await createPage(space, doc('a'))
      await edit(page, 'b', { userId: u.id })
      await edit(page, 'c', { userId: u.id })
      expect(await revs(page)).toHaveLength(2)
      await backdate(page)
      await edit(page, 'd', { userId: u.id })
      expect((await revs(page)).map(x => extractPlainText(x.content))).toEqual(['a', 'b', 'd'])
    })

    it('records a different author once the window has passed, preserving the previous author\'s unrecorded state', async () => {
      const space = await createSpace()
      const u1 = await createUser()
      const u2 = await createUser()
      const page = await createPage(space, doc('a'))
      await edit(page, 'b', { userId: u1.id })
      await edit(page, 'c', { userId: u1.id }) // debounced: only in pages.content
      await backdate(page)
      await edit(page, 'd', { userId: u2.id })
      const r = await revs(page)
      expect(r.map(x => [extractPlainText(x.content), x.author_user_id])).toEqual([
        ['a', null], ['b', u1.id], ['c', u1.id], ['d', u2.id],
      ])
    })

    it('records guest authors and never debounces unknown authors', async () => {
      const space = await createSpace()
      const u = await createUser()
      const page = await createPage(space, doc('a'))
      const link = await createShareLink(page, u.id, 'edit')
      await edit(page, 'b', { guestIdentityId: link.guestId })
      await edit(page, 'c', { guestIdentityId: link.guestId }) // same guest: debounced
      expect((await revs(page)).map(x => x.author_guest_id)).toEqual([null, link.guestId])
      await backdate(page)
      await edit(page, 'd') // unknown author
      await backdate(page)
      await edit(page, 'e')
      const r = await revs(page)
      expect(r.slice(-2).map(x => [extractPlainText(x.content), x.author_user_id, x.author_guest_id])).toEqual([
        ['d', null, null], ['e', null, null],
      ])
    })

    it('uses NULL for an author that does not exist instead of failing the store', async () => {
      const space = await createSpace()
      const page = await createPage(space, doc('a'))
      await edit(page, 'b', { userId: '00000000-0000-4000-8000-000000000001' })
      expect((await revs(page)).map(x => x.author_user_id)).toEqual([null, null])
    })

    it('records nothing when history is disabled for the page', async () => {
      const space = await createSpace()
      const page = await createPage(space, doc('a'))
      await pool.query('UPDATE pages SET history_enabled = false WHERE id = $1', [page])
      await edit(page, 'b')
      expect(await revs(page)).toHaveLength(0)
      expect(extractPlainText((await pageRow(page)).content)).toBe('b')
    })

    it('skips when content and title equal the latest revision', async () => {
      const space = await createSpace()
      const page = await createPage(space, doc('a'))
      await edit(page, 'b')
      const before = (await revs(page)).length
      // Latest revision (b) equals what we are about to write after a revert.
      await pool.query('UPDATE pages SET content = $2::jsonb WHERE id = $1', [page, JSON.stringify(doc('x'))])
      await pool.query('UPDATE collab_documents SET state = $2 WHERE page_id = $1', [page, Buffer.from(Y.encodeStateAsUpdate(await loadDoc(page)))])
      await edit(page, 'b')
      expect((await revs(page)).length).toBe(before)
    })
  })
})
