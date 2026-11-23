-- name: CreateSpace :one
INSERT INTO spaces (name, slug, icon, history_max_count, history_max_age_days)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSpaceBySlug :one
SELECT * FROM spaces WHERE slug = $1;

-- name: GetSpaceByID :one
SELECT * FROM spaces WHERE id = $1;

-- name: ListSpacesForUser :many
-- Direct membership only; group-based membership is resolved by joining
-- group_members separately where needed, kept out of this query to avoid
-- silently doubling a user's effective role via both paths. Returns the
-- member's role too, so the list can report my_role without N lookups.
SELECT s.*, sm.role
FROM spaces s
JOIN space_members sm ON sm.space_id = s.id
WHERE sm.user_id = $1
ORDER BY s.name;

-- name: GetSpaceMemberRole :one
SELECT role FROM space_members WHERE space_id = $1 AND user_id = $2;

-- name: ListAllSpaces :many
-- Org admins can see every space regardless of membership (SPEC.md §6.5) —
-- this is the admin-only counterpart to ListSpacesForUser.
SELECT * FROM spaces ORDER BY name;
