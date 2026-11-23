---
name: add-migration
description: This skill should be used when the user asks to "add a migration", "add a database migration", "add a column", "alter a table", "change the schema", "add an index", or "regenerate sqlc". Covers writing a golang-migrate up/down pair and regenerating sqlc code for little-doc-editor's Postgres schema.
---

# Adding a database migration

Applies to little-doc-editor's `api` service: golang-migrate for schema migrations, sqlc for type-safe query generation, Postgres FTS (`tsvector`/GIN + `pg_trgm`) for search. SPEC.md §3.1, §6.7, §6.8 and Decisions 2, 4, 9, 18 hold the authoritative domain rules — consult them for anything not covered here.

## Non-negotiable constraints

- The deployed Postgres is always external and bring-your-own (Decision 18) — never write a migration that assumes a bundled/dev-only database, and call out in the migration's comment if it needs an extension (e.g. `pg_trgm`) that requires `CREATE EXTENSION` privileges the operator's DB user may not have.
- Content is stored as ProseMirror/Tiptap JSON, not Markdown (Decision 4) — a migration adding searchable/exportable text should derive it from the JSON (via the Go layer or a generated column), not treat Markdown as the primary column.
- Multiple `api` replicas may run different binary versions briefly during a rolling deploy (Decision 3) — migrations must not require the new and old binary to disagree about a table's shape mid-rollout (see `references/rollout-safety.md`).

## Steps

1. **Create the migration pair.** Use the existing migrations directory's naming convention (check `ls` on the migrations folder for the pattern already in use — likely `<timestamp>_<description>.up.sql` / `.down.sql`). Write both files together; a migration without a working `down` is a defect unless the irreversibility is explicitly documented (e.g. an unrecoverable data-loss rollback).
2. **Write the `up` SQL.** Follow the domain model in SPEC.md §3.1 (`User`, `Group`, `Space`, `SpaceMember`, `Page`, `PageRevision`, `Attachment`, `ShareLink`, `ShareLinkAccessLog`, `Session`, `GuestIdentity`, `Invite`, `HistoryCleanupLog`). For a new nullable column, no default is needed; for `NOT NULL`, provide a default or a backfill step (see `references/rollout-safety.md` for why this matters with rolling deploys).
3. **Add indexes matching real query predicates.** Check the sqlc query files for how the new/changed column will actually be filtered, joined, or sorted before deciding what to index — don't index speculatively. If the column is searchable text, add a `tsvector` column + GIN index (Decision 2) and wire it through the `SearchIndexer`/`SearchQuerier` interface at the Go layer, not as raw SQL called directly from handlers.
4. **Write the `down` SQL.** Reverse exactly what `up` did, in reverse order (drop indexes before dropping columns they're on, etc.).
5. **Update sqlc query files** (`.sql` files sqlc reads) for any new/changed query shape, then regenerate:
   ```
   sqlc generate
   ```
   Check the sqlc config file (`sqlc.yaml`) if unsure of the invocation or output path.
6. **Update Go call sites** that use the regenerated types/queries, and update any domain struct in `internal/` (or equivalent) that mirrors the schema.
7. **If this touches history/lifecycle pruning**, re-check the max-count/max-age "whichever fires first" logic (Decision 9) still holds, and that the cleanup job only logs on an actual delete — this is a common place for an off-by-one or AND/OR mixup.

## Additional resources

- **`references/rollout-safety.md`** — why NOT NULL/rename/drop changes need a compatible rollout path under multi-replica deploys, with the specific patterns to use (add-nullable-then-backfill-then-constrain, expand/contract for renames).
