-- Backs ListPagesBySpace (WHERE space_id = $1 ORDER BY position, created_at, id)
-- so large spaces read the page tree in index order instead of sorting.
-- Plain CREATE INDEX: it blocks writes to pages for the build, which is short
-- at this table size; purely additive, so old binaries are unaffected.
CREATE INDEX pages_space_order_idx ON pages (space_id, position, created_at, id);
