import { randomUUID, createHash } from 'node:crypto'
import { afterAll, describe, expect, it } from 'vitest'
import { AccessDeniedError, authorizeDocument } from '../src/access.js'
import { verifySessionToken } from '../src/auth.js'
import { closePersistence, pool } from '../src/persistence.js'
import { addGroupMember, addMember, createPage, createShareLink, createSpace, createUser } from './fixtures.js'

afterAll(closePersistence)

describe('authorizeDocument', () => {
  it('maps space roles to read/write and rejects non-members', async () => {
    const space = await createSpace()
    const page = await createPage(space)
    const [editor, viewer, outsider, disabled, admin] = await Promise.all([
      createUser(), createUser(), createUser(), createUser({ disabled: true }), createUser({ orgAdmin: true }),
    ])
    await addMember(space, editor.id, 'editor')
    await addMember(space, viewer.id, 'viewer')
    await addMember(space, disabled.id, 'owner')
    const as = (u: { token: string }) => authorizeDocument(verifySessionToken(u.token), page)
    expect(await as(editor)).toEqual({ readOnly: false })
    expect(await as(viewer)).toEqual({ readOnly: true })
    expect(await as(admin)).toEqual({ readOnly: false })
    await expect(as(outsider)).rejects.toBeInstanceOf(AccessDeniedError)
    await expect(as(disabled)).rejects.toBeInstanceOf(AccessDeniedError)
  })

  it('uses the highest of direct and group-derived roles', async () => {
    const space = await createSpace()
    const page = await createPage(space)
    const u = await createUser()
    await addMember(space, u.id, 'viewer')
    expect(await authorizeDocument(verifySessionToken(u.token), page)).toEqual({ readOnly: true })
    await addGroupMember(space, u.id, 'editor')
    expect(await authorizeDocument(verifySessionToken(u.token), page)).toEqual({ readOnly: false })
    const onlyGroup = await createUser()
    await addGroupMember(space, onlyGroup.id, 'viewer')
    expect(await authorizeDocument(verifySessionToken(onlyGroup.token), page)).toEqual({ readOnly: true })
  })

  it('makes locked pages read-only and rejects non-canonical document names', async () => {
    const space = await createSpace()
    const page = await createPage(space, {}, { locked: true })
    const u = await createUser()
    await addMember(space, u.id, 'owner')
    expect(await authorizeDocument(verifySessionToken(u.token), page)).toEqual({ readOnly: true })
    await expect(authorizeDocument(verifySessionToken(u.token), page.toUpperCase())).rejects.toBeInstanceOf(AccessDeniedError)
    await expect(authorizeDocument(verifySessionToken(u.token), 'nope')).rejects.toBeInstanceOf(AccessDeniedError)
  })

  it('rejects revoked sessions (sessions.revoked_at deny-list)', async () => {
    const space = await createSpace()
    const page = await createPage(space)
    const u = await createUser()
    await addMember(space, u.id, 'editor')
    const identity = verifySessionToken(u.token)
    expect(await authorizeDocument(identity, page)).toEqual({ readOnly: false })
    await pool.query(
      `INSERT INTO sessions (user_id, token_hash, expires_at, revoked_at) VALUES ($1, $2, now() + interval '1 hour', now())`,
      [u.id, createHash('sha256').update(u.token).digest('hex')],
    )
    await expect(authorizeDocument(identity, page)).rejects.toThrow(/revoked/)
  })

  it('scopes share-link guests to the link page and its descendants, honouring revocation', async () => {
    const space = await createSpace()
    const owner = await createUser()
    await addMember(space, owner.id, 'owner')
    const parent = await createPage(space)
    const child = await createPage(space, {}, { parentId: parent })
    const other = await createPage(space)
    const edit = await createShareLink(parent, owner.id, 'edit')
    const view = await createShareLink(parent, owner.id, 'view')
    expect(await authorizeDocument(verifySessionToken(edit.token), child)).toEqual({ readOnly: false })
    expect(await authorizeDocument(verifySessionToken(view.token), parent)).toEqual({ readOnly: true })
    await expect(authorizeDocument(verifySessionToken(edit.token), other)).rejects.toBeInstanceOf(AccessDeniedError)
    await pool.query('UPDATE share_links SET revoked_at = now() WHERE id = $1', [edit.id])
    await expect(authorizeDocument(verifySessionToken(edit.token), parent)).rejects.toBeInstanceOf(AccessDeniedError)
    void randomUUID
  })
})
