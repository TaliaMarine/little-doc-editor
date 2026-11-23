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
