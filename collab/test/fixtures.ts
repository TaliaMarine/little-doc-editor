import { randomUUID } from 'node:crypto'
import jwt from 'jsonwebtoken'
import { pool } from '../src/persistence.js'

export function token (claims: Record<string, unknown>, expiresInSec = 3600): string {
  return jwt.sign(claims, 'test-secret', { algorithm: 'HS256', expiresIn: expiresInSec })
}

export async function createUser (opts: { disabled?: boolean, orgAdmin?: boolean } = {}) {
  const id = randomUUID()
  await pool.query(
    `INSERT INTO users (id, email, display_name, auth_provider, password_hash, disabled, is_org_admin)
     VALUES ($1, $2, $3, 'internal', 'x', $4, $5)`,
    [id, `${id}@test.local`, `User ${id.slice(0, 4)}`, opts.disabled ?? false, opts.orgAdmin ?? false],
  )
  return { id, displayName: `User ${id.slice(0, 4)}`, token: token({ sub: id, displayName: `User ${id.slice(0, 4)}` }) }
}

export async function createSpace () {
  const id = randomUUID()
  await pool.query('INSERT INTO spaces (id, name, slug) VALUES ($1, $2, $2)', [id, `s-${id}`])
  return id
}

export async function addMember (spaceId: string, userId: string, role: string) {
  await pool.query('INSERT INTO space_members (space_id, user_id, role) VALUES ($1, $2, $3)', [spaceId, userId, role])
}

export async function addGroupMember (spaceId: string, userId: string, role: string) {
  const groupId = randomUUID()
  await pool.query('INSERT INTO groups (id, name) VALUES ($1, $2)', [groupId, `g-${groupId}`])
  await pool.query('INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)', [groupId, userId])
  await pool.query('INSERT INTO space_members (space_id, group_id, role) VALUES ($1, $2, $3)', [spaceId, groupId, role])
}

export async function createPage (spaceId: string, content: unknown = {}, opts: { parentId?: string, locked?: boolean } = {}) {
  const id = randomUUID()
  await pool.query(
    'INSERT INTO pages (id, space_id, parent_id, title, content, locked) VALUES ($1, $2, $3, $4, $5::jsonb, $6)',
    [id, spaceId, opts.parentId ?? null, 'Test', JSON.stringify(content), opts.locked ?? false],
  )
  return id
}

export async function createShareLink (pageId: string, creatorId: string, permission: 'view' | 'edit') {
  const id = randomUUID()
  const guestId = randomUUID()
  await pool.query('INSERT INTO guest_identities (id, display_name) VALUES ($1, $2)', [guestId, 'Guest G'])
  await pool.query(
    'INSERT INTO share_links (id, page_id, token, permission, created_by_user_id) VALUES ($1, $2, $3, $4, $5)',
    [id, pageId, `t-${id}`, permission, creatorId],
  )
  return { id, guestId, token: token({ guestIdentityId: guestId, shareLinkId: id, displayName: 'Guest G' }) }
}

export const doc = (...paragraphs: string[]) => ({
  type: 'doc',
  content: paragraphs.map(t => ({ type: 'paragraph', content: [{ type: 'text', text: t }] })),
})
