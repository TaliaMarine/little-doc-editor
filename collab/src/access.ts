// Per-document authorization for collab connections, mirroring the api
// service's authz package (SPEC.md §6.5, Decisions 7, 21, 24) against the
// same Postgres tables. auth.ts only proves *who* is connecting; without this
// check any valid token — including a view-only share-link guest's — could
// open and write any page's document by guessing its ID.
//
// Evaluated at connect time, and re-evaluated (at most every few seconds) by
// server.ts's beforeHandleMessage hook, so revoking a share link, disabling a
// user, or demoting/removing a member also cuts off connections that are
// already open — matching api, which checks on every request.
import type { SessionIdentity } from './auth.js'
import { pool } from './persistence.js'

export interface DocumentAccess {
  // true for viewer-role users and view-permission share links, and for
  // locked pages — api rejects content writes in all three cases too.
  readOnly: boolean
}

// Implicit space role of every signed-in user (DEFAULT_MEMBER_ROLE, same
// setting as api's): '', 'viewer' or 'editor'. An explicit membership wins
// when it is higher.
const DEFAULT_MEMBER_ROLE = process.env.DEFAULT_MEMBER_ROLE === 'viewer' || process.env.DEFAULT_MEMBER_ROLE === 'editor'
  ? process.env.DEFAULT_MEMBER_ROLE
  : null
const ROLE_RANK = { viewer: 0, editor: 1, admin: 2, owner: 3 } as const

function effectiveRole (explicit: keyof typeof ROLE_RANK | null): keyof typeof ROLE_RANK | null {
  if (!DEFAULT_MEMBER_ROLE) {
    return explicit
  }
  if (explicit && ROLE_RANK[explicit] >= ROLE_RANK[DEFAULT_MEMBER_ROLE]) {
    return explicit
  }
  return DEFAULT_MEMBER_ROLE
}

export class AccessDeniedError extends Error {}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
// Document names must be the canonical lowercase form: Hocuspocus keys rooms
// by the exact string, so "AB12…" and "ab12…" would otherwise be two
// in-memory documents for the same page, each persisting over the other.
const CANONICAL_UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

// documentName is the page ID (see server.ts / persistence.ts).
export async function authorizeDocument (identity: SessionIdentity, documentName: string): Promise<DocumentAccess> {
  if (!CANONICAL_UUID_RE.test(documentName)) {
    throw new AccessDeniedError('document name is not a lowercase page id')
  }
  // Server-side logout (sessions.revoked_at deny-list, same rule as api's
  // IsSessionRevoked): a revoked token must stop working here too, including
  // on already-open sockets (re-checked by app.ts).
  if (identity.tokenHash) {
    const { rows } = await pool.query<{ revoked: boolean }>(
      'SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = $1 AND revoked_at IS NOT NULL) AS revoked',
      [identity.tokenHash],
    )
    if (rows[0]?.revoked) {
      throw new AccessDeniedError('session was revoked')
    }
  }
  if (identity.userId) {
    return authorizeUser(identity.userId, documentName)
  }
  if (identity.guestIdentityId && identity.shareLinkId) {
    return authorizeShareGuest(identity.guestIdentityId, identity.shareLinkId, documentName)
  }
  // Guest identities are only ever created via share-link redemption, so a
  // guest token without a link has no scope at all.
  throw new AccessDeniedError('guest session is not scoped to a share link')
}

async function authorizeUser (userId: string, pageId: string): Promise<DocumentAccess> {
  if (!UUID_RE.test(userId)) {
    throw new AccessDeniedError('invalid user id')
  }
  // The role is the highest of the user's direct membership and any role
  // granted to a group they belong to — same rule as api's
  // GetSpaceMemberRole (space_members.group_id / group_members).
  const { rows } = await pool.query<{
    disabled: boolean
    is_org_admin: boolean
    locked: boolean
    role: keyof typeof ROLE_RANK | null
  }>(
    `SELECT u.disabled, u.is_org_admin, p.locked,
       (SELECT sm.role::text FROM space_members sm
        WHERE sm.space_id = p.space_id
          AND (sm.user_id = u.id
               OR sm.group_id IN (SELECT gm.group_id FROM group_members gm WHERE gm.user_id = u.id))
        ORDER BY sm.role LIMIT 1) AS role
     FROM pages p
     JOIN users u ON u.id = $2
     WHERE p.id = $1`,
    [pageId, userId],
  )
  const row = rows[0]
  if (!row) {
    throw new AccessDeniedError('page or user not found')
  }
  if (row.disabled) {
    throw new AccessDeniedError('user is disabled')
  }
  // Org admins satisfy every space-level check (api's authz.HasSpaceRole).
  if (row.is_org_admin) {
    return { readOnly: row.locked }
  }
  const role = effectiveRole(row.role)
  if (role === null) {
    throw new AccessDeniedError('no role in this page\'s space')
  }
  return { readOnly: row.locked || role === 'viewer' }
}

async function authorizeShareGuest (guestIdentityId: string, shareLinkId: string, pageId: string): Promise<DocumentAccess> {
  if (!UUID_RE.test(guestIdentityId) || !UUID_RE.test(shareLinkId)) {
    throw new AccessDeniedError('invalid guest or share link id')
  }
  // The link covers its own page and every descendant (Decision 21): walk
  // up from the requested page and check the link's page is on that path.
  // UNION (not UNION ALL) dedupes rows, so a parent_id cycle terminates
  // instead of recursing until statement_timeout.
  const { rows } = await pool.query<{ permission: 'view' | 'edit', locked: boolean }>(
    `WITH RECURSIVE ancestors AS (
       SELECT id, parent_id FROM pages WHERE id = $1
       UNION
       SELECT p.id, p.parent_id FROM pages p JOIN ancestors a ON p.id = a.parent_id
     )
     SELECT sl.permission, (SELECT locked FROM pages WHERE id = $1) AS locked
     FROM share_links sl
     WHERE sl.id = $2
       AND sl.revoked_at IS NULL
       AND (sl.expires_at IS NULL OR sl.expires_at > now())
       AND sl.page_id IN (SELECT id FROM ancestors)
       AND EXISTS (SELECT 1 FROM guest_identities WHERE id = $3)`,
    [pageId, shareLinkId, guestIdentityId],
  )
  const row = rows[0]
  if (!row) {
    throw new AccessDeniedError('share link is inactive or does not cover this page')
  }
  return { readOnly: row.locked || row.permission !== 'edit' }
}
