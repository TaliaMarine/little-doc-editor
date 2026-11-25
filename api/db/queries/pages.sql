-- name: CreatePage :one
INSERT INTO pages (space_id, parent_id, position, title, content, created_by_user_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPageByID :one
SELECT * FROM pages WHERE id = $1;

-- name: ListChildPages :many
SELECT * FROM pages WHERE space_id = $1 AND parent_id = $2 ORDER BY position;

-- name: ListRootPages :many
SELECT * FROM pages WHERE space_id = $1 AND parent_id IS NULL ORDER BY position;

-- name: ListPagesBySpace :many
-- Backs the page-tree sidebar nav (SPEC.md §5): the whole space's pages in
-- one flat query, ordered by position — the frontend reconstructs the tree
-- from parent_id client-side rather than this being N per-level round trips.
-- Projects only nav-relevant columns (no content/search_vector — the nav
-- has no use for either, and content in particular can be large). The
-- (created_at, id) tiebreaker only matters for same-position rows (e.g.
-- both still at the column default before ever being reordered).
SELECT id, parent_id, position, title, locked, is_template, created_at, updated_at
FROM pages
WHERE space_id = $1
ORDER BY position, created_at, id;

-- name: GetMaxRootPagePosition :one
-- Used by create to append a new root page after existing siblings, rather
-- than leaving every page at the position column's zero default (which
-- made ListPagesBySpace's ordering meaningless — every row tied at 0).
SELECT COALESCE(MAX(position), -1)::double precision FROM pages WHERE space_id = $1 AND parent_id IS NULL;

-- name: GetMaxChildPagePosition :one
SELECT COALESCE(MAX(position), -1)::double precision FROM pages WHERE space_id = $1 AND parent_id = $2;

-- name: UpdatePageContent :one
UPDATE pages SET content = $2, title = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePagePosition :one
-- Backs page-tree drag-and-drop reordering/reparenting (SPEC.md §5). A
-- caller-supplied fractional position between the two flanking siblings —
-- computing that midpoint is the caller's job, not this query's.
UPDATE pages SET parent_id = $2, position = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdatePageSearchVector :exec
-- Called by the SearchIndexer after content changes, with plain text
-- extracted from the ProseMirror JSON on the Go side (Decision 2, §6.8) —
-- kept behind that interface rather than triggered in SQL so a future swap
-- to Meilisearch/Typesense only touches the Go layer.
UPDATE pages SET search_vector = to_tsvector('english', $2) WHERE id = $1;

-- name: SearchPages :many
-- Postgres FTS behind the Go layer's SearchIndexer/SearchQuerier interface
-- (Decision 2, SPEC.md §6.8) — this query is an implementation detail of
-- that interface, not called directly from handlers. Projects only what a
-- search result needs — SELECT * would pull every matching page's full
-- content jsonb and search_vector just to discard them.
SELECT id, title
FROM pages
WHERE space_id = $1
  AND search_vector @@ websearch_to_tsquery('english', $2)
ORDER BY ts_rank(search_vector, websearch_to_tsquery('english', $2)) DESC
LIMIT $3;

-- name: DeletePage :exec
DELETE FROM pages WHERE id = $1;

-- name: SetPageLocked :one
UPDATE pages SET locked = $2, updated_at = now() WHERE id = $1
RETURNING *;

-- name: GetPageByIDForUpdate :one
-- Row-locks the page inside a transaction (writers lock the page row first).
SELECT * FROM pages WHERE id = $1 FOR UPDATE;

-- name: CountLockedPagesInSubtree :one
-- Locked pages in a page's subtree (including the page itself).
WITH RECURSIVE tree AS (
    SELECT root.id AS id FROM pages root WHERE root.id = $1
    UNION
    SELECT child.id AS id FROM pages child JOIN tree t ON child.parent_id = t.id
)
SELECT count(*) FROM pages p JOIN tree ON p.id = tree.id WHERE p.locked;

-- name: ListAttachmentKeysForPageTree :many
-- S3 keys of every attachment on a page and its descendants, collected before
-- a delete cascades the rows away so the blobs can be removed too.
WITH RECURSIVE tree AS (
    SELECT root.id AS id FROM pages root WHERE root.id = $1
    UNION
    SELECT child.id AS id FROM pages child JOIN tree t ON child.parent_id = t.id
)
SELECT a.s3_key FROM attachments a JOIN tree ON a.page_id = tree.id;

-- name: ListAttachmentKeysForSpace :many
SELECT a.s3_key FROM attachments a JOIN pages p ON p.id = a.page_id WHERE p.space_id = $1;

-- name: ListPageSubtree :many
-- A page and all its descendants with content, for export. Depth-first
-- ordering is reconstructed in Go from parent_id/position.
WITH RECURSIVE tree AS (
    SELECT root.id AS id FROM pages root WHERE root.id = $1
    UNION
    SELECT child.id AS id FROM pages child JOIN tree t ON child.parent_id = t.id
)
SELECT p.id, p.space_id, p.parent_id, p.position, p.title, p.content, p.created_at
FROM pages p JOIN tree ON p.id = tree.id
ORDER BY p.position, p.created_at, p.id
LIMIT 2001; -- one past exportapi's page cap, so an oversized export is refused without loading it all

-- name: ListSpacePagesForExport :many
SELECT id, space_id, parent_id, position, title, content, created_at
FROM pages WHERE space_id = $1
ORDER BY position, created_at, id
LIMIT 2001; -- one past exportapi's page cap

-- name: CreatePageRevision :one
-- clock_timestamp(), not the column default now(): several revisions can be
-- written in one transaction (save + preserved state, restore) and must keep
-- their order, but now() is constant within a transaction.
INSERT INTO page_revisions (page_id, content, title, author_user_id, author_guest_id, created_at)
VALUES ($1, $2, $3, $4, $5, clock_timestamp())
RETURNING *;

-- name: GetLatestPageRevision :one
SELECT * FROM page_revisions WHERE page_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: ListPageRevisions :many
-- History list: metadata only (no content, which can be large).
SELECT r.id, r.page_id, r.title, r.author_user_id, r.author_guest_id, r.created_at,
       u.display_name AS author_user_display_name,
       g.display_name AS author_guest_display_name
FROM page_revisions r
LEFT JOIN users u ON u.id = r.author_user_id
LEFT JOIN guest_identities g ON g.id = r.author_guest_id
WHERE r.page_id = $1
ORDER BY r.created_at DESC, r.id DESC
LIMIT $2 OFFSET $3;

-- name: GetPageRevision :one
SELECT r.id, r.page_id, r.content, r.title, r.author_user_id, r.author_guest_id, r.created_at,
       u.display_name AS author_user_display_name,
       g.display_name AS author_guest_display_name
FROM page_revisions r
LEFT JOIN users u ON u.id = r.author_user_id
LEFT JOIN guest_identities g ON g.id = r.author_guest_id
WHERE r.id = $1 AND r.page_id = $2;

-- name: PruneSpaceRevisions :many
-- One space's lifecycle policy (Decision 9): a revision is deleted when it is
-- beyond the newest max_count of its page OR older than max_age_days —
-- whichever threshold is hit first. NULL means no limit on that dimension.
-- The newest revision of each page is never age-pruned, so an idle page keeps
-- its last known state (and a later save doesn't need a synthetic baseline).
-- Returns the deleted rows so the caller logs only when something was
-- actually deleted.
WITH ranked AS (
    SELECT r.id, r.created_at,
           row_number() OVER (PARTITION BY r.page_id ORDER BY r.created_at DESC, r.id DESC) AS rn
    FROM page_revisions r
    JOIN pages p ON p.id = r.page_id
    WHERE p.space_id = $1
)
DELETE FROM page_revisions pr
USING ranked
WHERE pr.id = ranked.id
  AND (
    (sqlc.narg('max_count')::integer IS NOT NULL AND ranked.rn > sqlc.narg('max_count')::integer)
    OR (sqlc.narg('max_age_days')::integer IS NOT NULL AND ranked.rn > 1
        AND ranked.created_at < now() - make_interval(days => sqlc.narg('max_age_days')::integer))
  )
RETURNING pr.id, pr.created_at;

-- name: CreateHistoryCleanupLog :exec
INSERT INTO history_cleanup_logs (space_id, deleted_count, oldest_deleted_at, newest_deleted_at)
VALUES ($1, $2, $3, $4);

-- name: UpdatePageTitle :one
-- Title-only rename: never touches content, so it can't clobber live
-- collaborative state (collab owns content while a doc is open).
UPDATE pages SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;
