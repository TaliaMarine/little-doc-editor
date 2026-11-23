-- name: CreateInvite :one
INSERT INTO invites (token, email, grants_org_admin, created_by_user_id, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInviteByToken :one
SELECT * FROM invites WHERE token = $1;

-- name: RedeemInvite :exec
UPDATE invites SET redeemed_at = now(), redeemed_by_user_id = $2 WHERE id = $1;

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC;
