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

-- name: CreatePageRevision :one
INSERT INTO page_revisions (page_id, content, author_user_id, author_guest_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListPageRevisions :many
SELECT * FROM page_revisions WHERE page_id = $1 ORDER BY created_at DESC;

-- name: PruneRevisionsBeyondCount :many
-- Deletes revisions beyond the space's history_max_count, keeping the most
-- recent N. Returns deleted rows so the caller can log deleted_count and the
-- oldest/newest deleted_at only when this actually deletes something
-- (Decision 9) — a zero-row result must not produce a history_cleanup_logs entry.
DELETE FROM page_revisions AS outer_pr
WHERE outer_pr.page_id = $1
  AND outer_pr.id IN (
    SELECT inner_pr.id FROM page_revisions AS inner_pr
    WHERE inner_pr.page_id = $1
    ORDER BY inner_pr.created_at DESC
    OFFSET $2
  )
RETURNING outer_pr.id, outer_pr.created_at;

-- name: PruneRevisionsBeyondAge :many
DELETE FROM page_revisions
WHERE page_id = $1 AND created_at < $2
RETURNING id, created_at;
