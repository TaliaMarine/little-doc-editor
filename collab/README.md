# collab

Real-time collaboration sidecar for little-doc-editor: Yjs documents served over WebSocket via [Hocuspocus](https://tiptap.dev/docs/hocuspocus), fanned out across replicas via Redis, with periodic snapshots persisted to the same Postgres database the `api` service uses. See the repo-root [SPEC.md](../SPEC.md) §4 and Decisions 1, 3, 20, 29.

## Current state

Working WS server with auth-on-connect (HS256 JWT, shared secret with `api` — `src/auth.ts`), per-document authorization (`src/access.ts`: the connecting user's role — direct or via group membership — or a share-link guest's live link scope decides whether they may open a page and whether the connection is read-only; revoked sessions (`sessions.revoked_at`) are refused), Redis fan-out, and Postgres persistence. Open connections are re-checked on every message (at most every `ACCESS_RECHECK_MS`, default 5s) and by a background sweep, so revocation/demotion/logout applies live, even to idle sockets.

### Content ownership (Yjs <-> pages.content)

- **collab owns the Yjs <-> ProseMirror JSON conversion** (`src/convert.ts`, schema in `src/schema.ts`); `api` stays CRDT-agnostic (Decision 1).
- **Seeding**: on first load of a page with no `collab_documents` row, `onLoadDocument` converts `pages.content` into the Yjs doc and writes the seed row immediately. The seed uses a fixed Yjs client ID, so replicas that seed concurrently produce identical updates that merge without duplicating content. `{}` / empty docs seed blank. Unknown nodes/marks in stored JSON are unwrapped/dropped rather than crashing; content that cannot be converted at all fails the load (so a blank doc never overwrites real content).
- **Write-back**: every store (debounced by Hocuspocus, retried with exponential backoff — `STORE_MAX_ATTEMPTS`, `STORE_RETRY_BASE_MS`) merges the Yjs snapshot into `collab_documents` and, in the same transaction, updates `pages.content`, `pages.search_vector` (`to_tsvector('english', <plain text>)`, plain text extracted exactly like `api/internal/searchindex.ExtractPlainText`) and `pages.updated_at` (only when the content actually changed; title is never touched). A Yjs document that contains nodes/marks outside `schema.ts` still has its snapshot saved but skips the write-back (counted in `collab_writeback_failures_total`) rather than deleting the unknown parts from `pages.content`.
- **Revisions (Decision 42)**: when a store changes `pages.content`, collab also inserts into `page_revisions` in the same transaction, with api's `revisions.Recorder.OnSave` semantics: it holds the `pages` row lock (api locks it first too); `created_at = clock_timestamp()`; skipped when `pages.history_enabled = false`, when content+title equal the latest revision, or when the latest revision (by **any** author, a deliberate deviation from api's same-author rule: a live store's author is only whoever made the last change, so with several editors it would flip and write a revision every 2-10s) is less than `HISTORY_REVISION_DEBOUNCE` ago (default `5m`, Go-style duration, `0` = off). A page with no revision yet first gets its pre-save state recorded as a baseline (under its creator); a different author saving over the previous author's unrecorded state first records that state under the previous author. The author is the user/guest of the connection that made the last change (Hocuspocus `lastContext`); unknown or deleted identities, or updates that arrived via Redis from another replica without context, are stored as NULL/NULL, which never debounces.
- **Manual `PUT /pages/{id}/content`**: while holding row locks, collab compares `pages.content` with what its stored snapshot represents (both normalised through the same schema/Yjs round trip). If they differ, a manual write happened since the last store: the manual content wins, collab merges it into the snapshot under row locks as real Yjs delete+insert operations (never a fresh seed, which would corrupt peers' Yjs IDs) and applies it to the live document (or on next load if nobody is connected) — last write wins, edits typed in the last debounce window are lost. A PUT that merely echoes the editor state (the Save button) is a no-op for the live doc. To make this immediate rather than waiting for the next store, api publishes after committing a restore or a content-changing PUT (see below).
- **Content-changed notification** (`src/contentSync.ts`): collab subscribes to the Redis channel `lde:page-content-replaced` (constant `PAGE_CONTENT_CHANNEL`; api: `internal/pagesync.Channel`). The payload is just the page id (UUID). On a message, every replica holding a live room for that page runs the manual-write merge above at once (`storeDocumentState(..., { onlyExternal: true })`: writes nothing unless the content really differs, so the editor's own Save echo is a no-op) and applies the result to the in-memory document, which Hocuspocus broadcasts to clients (and, via the Redis extension, to other replicas). No live room on that replica = no-op (the next load merges it); invalid payloads are ignored. Delivery is best-effort: if the message is lost, behaviour falls back to the next-store detection. collab needs `SUBSCRIBE` on the channel (and api `PUBLISH`) if Redis ACLs are used.
- **Schema sync**: `src/schema.ts` must define the same nodes/marks/attributes as the frontend's shared extensions module (StarterKit with undo/redo disabled, `attachmentImage` node with `attachmentId`/`alt`, Mermaid as `codeBlock` with `language: 'mermaid'`). Yjs fragment name is `default`.

### Presence

Awareness updates are rewritten server-side (`src/awareness.ts`): `user.name` is pinned to the token's display name, only `user.color` (hex) and `cursor` survive, and read-only connections get no cursor state.

### Metrics

`GET /metrics` (Prometheus), on the HTTP port by default or, with `METRICS_PORT` set, on a separate metrics-only listener: `collab_connections`, `collab_documents`, `collab_store_failures_total`, `collab_store_duration_seconds`, `collab_writeback_failures_total`, `collab_external_content_replacements_total`, `collab_access_revocations_total` plus default process metrics. Unauthenticated; requests carrying `X-Forwarded-*`/`Forwarded` headers (i.e. via ingress) get 404, so scrape it via the cluster-internal Service only. Setting `METRICS_PORT` (default unset/0 = off, old behaviour) serves `/metrics` only on that extra port (all other paths 404) and no longer on the main port, so a public ingress that targets the WebSocket port can never reach it, whatever headers it sends (like api's `METRICS_ADDR`). Session JWTs must be canonically spelled (unpadded base64url that re-encodes to itself, three segments) since the revoked-sessions lookup hashes the exact token string; re-encoded variants are rejected. OTLP tracing is not implemented.

Not built yet: a JWKS-based alternative to the shared secret.

## Local development

Requires Node 24+, a running Postgres (with the api service's migrations applied), and a running Redis.

```sh
cp .env.example .env
export $(cat .env | xargs)
npm install
npm run dev
```

## Tests

`npm test` (vitest). Unit tests need nothing; the DB tests and the WebSocket test (`@hocuspocus/provider` against a real in-process server) need a migrated Postgres. Use a throwaway database: `createdb lde_collab`, run api's migrations against it (`cd ../api && DATABASE_URL=... go run ./cmd/migrate -direction up`), and set `TEST_DATABASE_URL` if it is not `postgres://postgres:postgres@localhost:5432/lde_collab?sslmode=disable`. Tests create their own rows and never clean up, so don't point them at a real database.

## Notes

- `SESSION_JWT_SECRET` must match what `api` signs session tokens with.
- `COLLAB_ALLOWED_ORIGINS` (optional, comma-separated): reject a WebSocket handshake whose `Origin` isn't listed (403); unset = no check; no `Origin` header = not a browser, token auth decides (Decision 48).
- Redis: `REDIS_URL` (`redis://` or `rediss://`, with optional `user:pass@` and `/db`) or `REDIS_ADDR` (`host`, `host:port`, `[v6]:port`); optional `REDIS_USERNAME`/`REDIS_PASSWORD` (override the URL's) and `REDIS_TLS=true` (implied by `rediss://`). `REDIS_PASSWORD` is the Redis AUTH password; leave unset for a Redis without auth. This is the "shared secret" half of the "shared secret or JWKS check" option in SPEC.md §4 — switching to JWKS would mean validating against api's public signing key instead of a shared secret, without changing the connection/room-join flow.
- Redis and Postgres are both bring-your-own (Decisions 18, 29) — nothing here spins up a local instance of either.
