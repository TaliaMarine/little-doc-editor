-- name: CreateUserSession :one
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateGuestSession :one
INSERT INTO sessions (guest_identity_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateGuestIdentity :one
INSERT INTO guest_identities (display_name)
VALUES ($1)
RETURNING *;

-- name: GetGuestIdentityByID :one
SELECT * FROM guest_identities WHERE id = $1;

-- name: IsSessionRevoked :one
-- Deny-list lookup used by authctx.Middleware on every request.
SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = $1 AND revoked_at IS NOT NULL);

-- name: RevokeSession :exec
-- Logout. Upserts so a token whose login-time row was never recorded (the
-- recording is best-effort) can still be revoked.
INSERT INTO sessions (user_id, guest_identity_id, token_hash, expires_at, revoked_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (token_hash) DO UPDATE SET revoked_at = COALESCE(sessions.revoked_at, now());

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at < now();
