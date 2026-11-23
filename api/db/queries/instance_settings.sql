-- name: GetInstanceSettings :one
SELECT * FROM instance_settings WHERE id = true;

-- name: SetInternalLoginDisabled :one
UPDATE instance_settings SET internal_login_disabled = $1, updated_at = now() WHERE id = true
RETURNING *;

-- name: CountOrgAdmins :one
SELECT count(*) FROM users WHERE is_org_admin = true AND disabled = false;
