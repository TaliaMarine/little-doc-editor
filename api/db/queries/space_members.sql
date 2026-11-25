-- name: AddSpaceMemberUser :one
INSERT INTO space_members (space_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (space_id, user_id) DO UPDATE SET role = $3
RETURNING *;

-- name: AddSpaceMemberGroup :one
INSERT INTO space_members (space_id, group_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (space_id, group_id) DO UPDATE SET role = $3
RETURNING *;

-- name: ListSpaceMembers :many
SELECT sm.id, sm.space_id, sm.user_id, sm.group_id, sm.role, sm.created_at,
       u.email AS user_email, u.display_name AS user_display_name,
       g.name AS group_name
FROM space_members sm
LEFT JOIN users u ON u.id = sm.user_id
LEFT JOIN groups g ON g.id = sm.group_id
WHERE sm.space_id = $1
ORDER BY sm.created_at;

-- name: GetSpaceMemberUser :one
SELECT * FROM space_members WHERE space_id = $1 AND user_id = $2;

-- name: GetSpaceMemberGroup :one
SELECT * FROM space_members WHERE space_id = $1 AND group_id = $2;

-- name: UpdateSpaceMemberRole :one
UPDATE space_members SET role = $3 WHERE space_id = $1 AND user_id = $2
RETURNING *;

-- name: UpdateSpaceMemberGroupRole :one
UPDATE space_members SET role = $3 WHERE space_id = $1 AND group_id = $2
RETURNING *;

-- name: RemoveSpaceMemberUser :execrows
DELETE FROM space_members WHERE space_id = $1 AND user_id = $2;

-- name: RemoveSpaceMemberGroup :execrows
DELETE FROM space_members WHERE space_id = $1 AND group_id = $2;

-- name: CountSpaceOwners :one
-- Direct (user) owners only: a group's membership changes with the IdP, so a
-- group owner can't be what keeps a space from being ownerless.
SELECT count(*) FROM space_members WHERE space_id = $1 AND role = 'owner' AND user_id IS NOT NULL;

-- name: ListGroups :many
SELECT id, name, created_at FROM groups
WHERE (sqlc.narg('q')::text IS NULL OR name ILIKE '%' || sqlc.narg('q')::text || '%')
ORDER BY lower(name)
LIMIT $1;

-- name: GetGroupByID :one
SELECT * FROM groups WHERE id = $1;

-- name: SyncUserGroups :exec
-- Mirrors the IdP's group claim for one user in a single statement (atomic):
-- unknown groups are created, memberships the IdP no longer reports are
-- dropped, current ones added. Data-modifying CTEs share one snapshot, so
-- groups created here are picked up via the INSERT's RETURNING.
WITH created AS (
    INSERT INTO groups (name)
    SELECT unnest(@names::text[])
    ON CONFLICT (name) DO NOTHING
    RETURNING id
), dropped AS (
    DELETE FROM group_members
    WHERE user_id = @user_id::uuid
      AND group_id NOT IN (SELECT g.id FROM groups g WHERE g.name = ANY(@names::text[]))
)
INSERT INTO group_members (group_id, user_id)
SELECT id, @user_id::uuid FROM created
UNION
SELECT g.id, @user_id::uuid FROM groups g WHERE g.name = ANY(@names::text[])
ON CONFLICT DO NOTHING;
