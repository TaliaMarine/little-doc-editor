-- name: CreateSpace :one
INSERT INTO spaces (name, slug, icon, history_max_count, history_max_age_days)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSpaceBySlug :one
SELECT * FROM spaces WHERE slug = $1;

-- name: GetSpaceByID :one
SELECT * FROM spaces WHERE id = $1;

-- name: LockSpace :one
-- Serializes member-management changes (last-owner guard) within a transaction.
SELECT id FROM spaces WHERE id = $1 FOR UPDATE;

-- name: UpdateSpace :one
-- Each field is applied only when its set_* flag is true, so a PATCH can
-- distinguish "leave alone" from "set to null" (no limit).
UPDATE spaces SET
    name                 = CASE WHEN sqlc.arg('set_name')::boolean THEN sqlc.arg('name')::text ELSE name END,
    icon                 = CASE WHEN sqlc.arg('set_icon')::boolean THEN sqlc.narg('icon')::text ELSE icon END,
    history_max_count    = CASE WHEN sqlc.arg('set_max_count')::boolean THEN sqlc.narg('history_max_count')::integer ELSE history_max_count END,
    history_max_age_days = CASE WHEN sqlc.arg('set_max_age')::boolean THEN sqlc.narg('history_max_age_days')::integer ELSE history_max_age_days END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSpace :exec
DELETE FROM spaces WHERE id = $1;

-- name: ListSpacesForUser :many
-- Direct and group-derived membership, at the highest role per space (the
-- space_role enum is declared owner < admin < editor < viewer, so min() is
-- the most privileged). Returns the role too, so the list can report my_role
-- without N lookups.
SELECT s.*, (min(r.role))::space_role AS role
FROM spaces s
JOIN (
    SELECT sm.space_id, sm.role FROM space_members sm WHERE sm.user_id = $1
    UNION ALL
    SELECT sm.space_id, sm.role
    FROM space_members sm
    JOIN group_members gm ON gm.group_id = sm.group_id
    WHERE gm.user_id = $1
) r ON r.space_id = s.id
GROUP BY s.id
ORDER BY s.name;

-- name: GetSpaceMemberRole :one
-- Highest of the user's direct role and any role granted to a group they
-- belong to (enum order: owner first). collab/src/access.ts must use the
-- same rule.
SELECT role FROM (
    SELECT sm.role FROM space_members sm WHERE sm.space_id = $1 AND sm.user_id = $2
    UNION ALL
    SELECT sm.role
    FROM space_members sm
    JOIN group_members gm ON gm.group_id = sm.group_id
    WHERE sm.space_id = $1 AND gm.user_id = $2
) r
ORDER BY role
LIMIT 1;

-- name: ListAllSpaces :many
-- Org admins can see every space regardless of membership (SPEC.md §6.5) —
-- this is the admin-only counterpart to ListSpacesForUser.
SELECT * FROM spaces ORDER BY name;

-- name: ListSpacesWithHistoryPolicy :many
SELECT * FROM spaces WHERE history_max_count IS NOT NULL OR history_max_age_days IS NOT NULL;
