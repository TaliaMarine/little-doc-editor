-- name: CreateInvite :one
-- token_hash is the hex SHA-256 of the bearer token; the plaintext is never stored.
INSERT INTO invites (token_hash, email, grants_org_admin, created_by_user_id, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInviteByTokenHash :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: ClaimInvite :one
-- Atomically consumes a usable invite (not redeemed, not revoked, not
-- expired), so two concurrent redemptions can't both win. Run inside the
-- same transaction that creates the user, so a failed user insert (e.g. an
-- email conflict) rolls the claim back.
UPDATE invites SET redeemed_at = now()
WHERE token_hash = $1
  AND redeemed_at IS NULL
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
RETURNING *;

-- name: SetInviteRedeemedBy :exec
UPDATE invites SET redeemed_by_user_id = $2 WHERE id = $1;

-- name: ListInvites :many
SELECT i.id, i.email, i.grants_org_admin, i.created_by_user_id, i.expires_at,
       i.redeemed_at, i.redeemed_by_user_id, i.revoked_at, i.created_at,
       cu.display_name AS created_by_display_name
FROM invites i
LEFT JOIN users cu ON cu.id = i.created_by_user_id
ORDER BY i.created_at DESC;

-- name: GetInviteByID :one
SELECT * FROM invites WHERE id = $1;

-- name: RevokeInvite :one
-- Only a still-pending invite can be revoked; returns no row otherwise.
UPDATE invites SET revoked_at = now()
WHERE id = $1 AND redeemed_at IS NULL AND revoked_at IS NULL
RETURNING *;
