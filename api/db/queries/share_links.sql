-- name: CreateShareLink :one
INSERT INTO share_links (page_id, token, permission, password_hash, expires_at, created_by_user_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetShareLinkByToken :one
SELECT * FROM share_links WHERE token = $1;

-- name: GetShareLinkByID :one
SELECT * FROM share_links WHERE id = $1;

-- name: ListShareLinksForPage :many
SELECT * FROM share_links WHERE page_id = $1 ORDER BY created_at DESC;

-- name: RevokeShareLink :exec
UPDATE share_links SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: CreateShareLinkAccessLog :one
INSERT INTO share_link_access_logs (share_link_id, ip_hash, user_agent, viewer_user_id, viewer_guest_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListShareLinkAccessLogs :many
SELECT l.id, l.share_link_id, l.accessed_at, l.ip_hash, l.user_agent,
       l.viewer_user_id, l.viewer_guest_id,
       u.display_name AS viewer_user_display_name,
       g.display_name AS viewer_guest_display_name
FROM share_link_access_logs l
LEFT JOIN users u ON u.id = l.viewer_user_id
LEFT JOIN guest_identities g ON g.id = l.viewer_guest_id
WHERE l.share_link_id = $1
ORDER BY l.accessed_at DESC, l.id DESC
LIMIT $2 OFFSET $3;

-- name: ListPageDescendantIDs :many
-- Share-link scope defaults to the page and its descendants (Decision 21).
-- Recursive CTE walking parent_id — fine at this schema's expected page-tree
-- depth/fanout; revisit with a materialized path/closure table if a space
-- ever has pathologically deep trees.
WITH RECURSIVE descendants AS (
    SELECT root.id AS id FROM pages root WHERE root.id = $1
    UNION
    SELECT child.id AS id FROM pages child
    JOIN descendants d ON child.parent_id = d.id
)
SELECT descendants.id FROM descendants;
