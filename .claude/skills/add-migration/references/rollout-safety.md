# Rollout-safe schema changes

little-doc-editor runs multiple `api` replicas in production (SPEC.md Decision 3), and Helm upgrades roll pods one at a time. For a window during every deploy, old and new binaries run against the same database simultaneously. A migration that isn't safe for that window will cause errors or data corruption during rollout, not just at some hypothetical future point.

## Adding a NOT NULL column

Don't do this in one step:
```sql
ALTER TABLE pages ADD COLUMN template_id uuid NOT NULL;
```
The old binary doesn't know about `template_id` and will fail every insert once this lands, whether or not it also has a default.

Do this instead, split across two migrations/deploys if the column has no natural default:
1. Add the column nullable (optionally with a default for new rows): `ALTER TABLE pages ADD COLUMN template_id uuid;`
2. Backfill existing rows in a follow-up step (batched if the table is large).
3. Only once all replicas are running code that always populates it, add the `NOT NULL` constraint in a later migration: `ALTER TABLE pages ALTER COLUMN template_id SET NOT NULL;`

If a single migration is genuinely safe (e.g. a new table nothing depends on yet, or a column with a constant default that's cheap on the target Postgres version), it's fine to do it in one step — the point is to reason about it, not follow the split pattern reflexively.

## Renaming or dropping a column

Never rename in place — the old binary will break instantly. Use expand/contract:
1. Add the new column.
2. Deploy code that writes to both old and new columns, reads from whichever is populated (or backfill then switch reads to new).
3. Once fully rolled out and backfilled, deploy code that only uses the new column.
4. Only then, in a later migration, drop the old column.

## Changing a column's type

Same expand/contract approach: add a new column of the new type, dual-write/backfill, cut over reads, then drop the old column. Don't rely on Postgres's `ALTER COLUMN ... TYPE` in place unless the old and new type are read-compatible by both old and new binary code paths during the rollout window.

## Indexes

Adding an index is generally safe for rollout (doesn't change reads/writes' correctness), but on a large table it can lock writes — prefer `CREATE INDEX CONCURRENTLY` and note that it can't run inside the same transaction as other migration statements in that file; golang-migrate runs each migration file in a transaction by default, so a concurrent index build usually needs its own migration file with the transactional wrapper disabled if the tool supports it, or should be flagged to the operator to run manually with a documented runbook step.
