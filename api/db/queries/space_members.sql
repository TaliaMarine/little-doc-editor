-- name: AddSpaceMemberUser :one
INSERT INTO space_members (space_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (space_id, user_id) DO UPDATE SET role = $3
RETURNING *;

-- name: ListSpaceMembers :many
SELECT sm.id, sm.space_id, sm.user_id, sm.group_id, sm.role, sm.created_at,
       u.email AS user_email, u.display_name AS user_display_name
FROM space_members sm
LEFT JOIN users u ON u.id = sm.user_id
WHERE sm.space_id = $1
ORDER BY sm.created_at;

-- name: UpdateSpaceMemberRole :one
UPDATE space_members SET role = $3 WHERE space_id = $1 AND user_id = $2
RETURNING *;

-- name: RemoveSpaceMemberUser :exec
DELETE FROM space_members WHERE space_id = $1 AND user_id = $2;
