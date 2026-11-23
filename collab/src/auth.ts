// Validates the same session/JWT that the api service issues, before a
// client is allowed to join a document room (SPEC.md §4). This shares only a
// signing secret with api — it does not call back into api on every
// connect, keeping room joins fast, but it means the token format here must
// stay in lockstep with api's internal/sessiontoken. Authentication only —
// whether this identity may open a given document is access.ts's job.
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
  displayName: string
  // Token expiry (ms since epoch) — re-checked per message by server.ts, so
  // a long-lived socket can't outlive the token it was opened with.
  expiresAt: number
}

export class AuthError extends Error {}

export function verifySessionToken(token: string): SessionIdentity {
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

  return { userId: sub, guestIdentityId, shareLinkId, displayName, expiresAt: payload.exp * 1000 }
}
