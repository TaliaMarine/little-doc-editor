// Validates the same session/JWT that the api service issues, before a
// client is allowed to join a document room (SPEC.md §4). This shares only a
// signing secret with api — it does not call back into api on every
// connect, keeping room joins fast, but it means the token format here must
// stay in lockstep with api's internal/sessiontoken. Authentication only —
// whether this identity may open a given document is access.ts's job.
import { createHash } from 'node:crypto'
import jwt from 'jsonwebtoken'
import { config } from './config.js'

export interface SessionIdentity {
  // Exactly one of these is set — a session backs either a user or a guest
  // identity, never both (mirrors the `sessions` table's CHECK constraint).
  userId?: string
  guestIdentityId?: string
  // Set only for a guest who redeemed a share link (api's
  // sessiontoken.IssueForShareGuest). The link's page scope and permission
  // are looked up from share_links at connect time (see access.ts), not
  // carried in the token, so revoked/expired links stop working.
  shareLinkId?: string
  // Hex SHA-256 of the raw token = sessions.token_hash (api's
  // sessionrecord.HashToken), for the revoked_at deny-list check.
  tokenHash: string
  displayName: string
  // Token expiry (ms since epoch) — re-checked per message by server.ts, so
  // a long-lived socket can't outlive the token it was opened with.
  expiresAt: number
}

export class AuthError extends Error {}

// Rejects any spelling of the token other than the canonical one jwt libraries
// emit: exactly three unpadded base64url segments that re-encode to
// themselves. Node's base64 decoder is lenient (ignores stray characters and
// non-zero trailing bits), so a re-spelled token would still verify; but the
// revoked-sessions lookup keys on a hash of the exact token string, so a
// variant must not get past it. Mirrors api's jwt.WithStrictDecoding.
export function isCanonicalJwt(token: string): boolean {
  const parts = token.split('.')
  if (parts.length !== 3) return false
  return parts.every(p => p.length > 0 && Buffer.from(p, 'base64url').toString('base64url') === p)
}

export function verifySessionToken(token: string): SessionIdentity {
  if (!isCanonicalJwt(token)) {
    throw new AuthError('invalid session token: non-canonical encoding')
  }
  let payload: jwt.JwtPayload
  try {
    // Pin the algorithm to what api actually signs with (HS256) rather than
    // accepting whatever the token header claims.
    const decoded = jwt.verify(token, config.sessionJwtSecret, { algorithms: ['HS256'] })
    if (typeof decoded === 'string') {
      throw new AuthError('malformed token payload')
    }
    payload = decoded
  } catch (err) {
    throw new AuthError(`invalid session token: ${(err as Error).message}`)
  }

  const { sub, guestIdentityId, shareLinkId, displayName } = payload as {
    sub?: string
    guestIdentityId?: string
    shareLinkId?: string
    displayName?: string
  }

  if (!displayName) {
    throw new AuthError('token missing displayName')
  }
  if (!sub && !guestIdentityId) {
    throw new AuthError('token has neither sub nor guestIdentityId')
  }
  if (sub && guestIdentityId) {
    throw new AuthError('token has both sub and guestIdentityId')
  }

  if (payload.exp === undefined) {
    throw new AuthError('token has no expiry')
  }

  return { userId: sub, guestIdentityId, shareLinkId, displayName, tokenHash: createHash('sha256').update(token).digest('hex'), expiresAt: payload.exp * 1000 }
}
