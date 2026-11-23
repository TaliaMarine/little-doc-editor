-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByOIDCIdentity :one
SELECT * FROM users WHERE oidc_issuer = $1 AND oidc_subject = $2;

-- name: CreateInternalUser :one
INSERT INTO users (email, display_name, auth_provider, password_hash)
VALUES ($1, $2, 'internal', $3)
RETURNING *;

-- name: CreateOIDCUser :one
INSERT INTO users (email, display_name, auth_provider, oidc_issuer, oidc_subject)
VALUES ($1, $2, 'oidc', $3, $4)
RETURNING *;

-- name: SetUserDisabled :exec
UPDATE users SET disabled = $2, updated_at = now() WHERE id = $1;

-- name: CreateInternalOrgAdmin :one
-- Used only for the bootstrap admin (Decision 11/14) — the first internal
-- account, created directly as an org admin since there's no other admin
-- yet to promote it.
INSERT INTO users (email, display_name, auth_provider, password_hash, is_org_admin)
VALUES ($1, $2, 'internal', $3, true)
RETURNING *;

-- name: UpsertDevAdminUser :one
-- Only called when DEV_AUTH_BYPASS is enabled (see internal/authctx), to
-- give the fixed bypass identity a real row so FK-bearing tables (pages,
-- attachments, ...) have something valid to reference. password_hash is a
-- real hash of a well-known dev password so the frontend's login page (which
-- needs a real token to get past its router guard) works under the bypass.
-- Re-enabled here because startup with the bypass OFF disables this row (see
-- cmd/api/main.go) so the well-known password can't be used outside it.
INSERT INTO users (id, email, display_name, auth_provider, password_hash, is_org_admin)
VALUES ($1, $2, $3, 'internal', $4, true)
ON CONFLICT (id) DO UPDATE SET display_name = $3, password_hash = $4, is_org_admin = true, disabled = false
RETURNING *;

-- name: GetUserByEmailAnyProvider :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: UpdateOIDCUserProfile :one
-- Applied on every OIDC login: the IdP is the source of truth for the display
-- name, profile fields, group membership and (via the admin group) org admin.
UPDATE users
SET display_name  = $2,
    username      = $3,
    avatar_url    = $4,
    idp_groups    = $5,
    is_org_admin  = $6,
    last_login_at = now(),
    updated_at    = now()
WHERE id = $1
RETURNING *;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;
