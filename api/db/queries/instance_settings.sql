-- name: GetInstanceSettings :one
SELECT * FROM instance_settings WHERE id = true;

-- name: UpdateInstanceSettings :one
-- One statement so a PATCH touching several settings applies all or none. Each
-- field changes only when its set_* flag is true; for the attachment policy a
-- NULL value with the flag set resets that field to the deployment default
-- (Decision 17).
UPDATE instance_settings SET
    internal_login_disabled   = CASE WHEN sqlc.arg('set_internal_login')::boolean THEN sqlc.arg('internal_login_disabled')::boolean ELSE internal_login_disabled END,
    attachment_allowlist      = CASE WHEN sqlc.arg('set_allowlist')::boolean THEN sqlc.narg('allowlist')::text[] ELSE attachment_allowlist END,
    attachment_max_size_bytes = CASE WHEN sqlc.arg('set_max_size')::boolean THEN sqlc.narg('max_size_bytes')::bigint ELSE attachment_max_size_bytes END,
    updated_at = now()
WHERE id = true
RETURNING *;

-- name: CountOrgAdmins :one
SELECT count(*) FROM users WHERE is_org_admin = true AND disabled = false;
