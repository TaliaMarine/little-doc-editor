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

-- name: ListUsers :many
-- Org-admin user directory. q (optional) filters by email/display name.
SELECT id, email, display_name, auth_provider, is_org_admin, disabled, created_at, last_login_at
FROM users
WHERE (sqlc.narg('q')::text IS NULL
       OR email ILIKE '%' || sqlc.narg('q')::text || '%'
       OR display_name ILIKE '%' || sqlc.narg('q')::text || '%')
ORDER BY lower(display_name), email
LIMIT $1 OFFSET $2;

-- name: SearchMemberCandidates :many
-- Enabled users that are not yet a direct member of the space — the picker a
-- space admin uses to add a member (no org-admin rights needed).
SELECT u.id, u.email, u.display_name
FROM users u
WHERE u.disabled = false
  AND (u.email ILIKE '%' || sqlc.arg('q')::text || '%' OR u.display_name ILIKE '%' || sqlc.arg('q')::text || '%')
  AND NOT EXISTS (SELECT 1 FROM space_members sm WHERE sm.space_id = sqlc.arg('space_id') AND sm.user_id = u.id)
ORDER BY lower(u.display_name), u.email
LIMIT $1;

-- name: LockUserRow :one
-- Serializes concurrent admin changes (last-org-admin guard) on one user row.
SELECT id FROM users WHERE id = $1 FOR UPDATE;

-- name: LockOrgAdminChanges :exec
-- Transaction-scoped advisory lock serializing changes that could remove the
-- last enabled org admin.
SELECT pg_advisory_xact_lock(hashtext('lde:org-admin-changes'));
