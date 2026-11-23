# collab

Real-time collaboration sidecar for little-doc-editor: Yjs documents served over WebSocket via [Hocuspocus](https://tiptap.dev/docs/hocuspocus), fanned out across replicas via Redis, with periodic snapshots persisted to the same Postgres database the `api` service uses. See the repo-root [SPEC.md](../SPEC.md) §4 and Decisions 1, 3, 20, 29.

## Current state

Working WS server with auth-on-connect (HS256 JWT, shared secret with `api` — `src/auth.ts`), per-document authorization (`src/access.ts`: the connecting user's space role, or a share-link guest's live link scope, decides whether they may open a page and whether the connection is read-only; re-checked on open connections every few seconds, so revocation/demotion applies live), Redis fan-out, and snapshot persistence to the `collab_documents` table (see `api/db/migrations/000002_collab_documents.up.sql`). Not built yet: seeding a new Yjs doc from an existing page's `pages.content`, writing live content back to `pages.content` (see NEXT.md §3), a JWKS-based alternative to the shared secret, and `/metrics`.

## Local development

Requires Node 24+, a running Postgres (with the api service's migrations applied), and a running Redis.

```sh
cp .env.example .env
export $(cat .env | xargs)
npm install
npm run dev
```

## Notes

- `SESSION_JWT_SECRET` must match what `api` signs session tokens with.
- `REDIS_PASSWORD` (optional) is the Redis AUTH password; leave unset for a Redis without auth. This is the "shared secret" half of the "shared secret or JWKS check" option in SPEC.md §4 — switching to JWKS would mean validating against api's public signing key instead of a shared secret, without changing the connection/room-join flow.
- Redis and Postgres are both bring-your-own (Decisions 18, 29) — nothing here spins up a local instance of either.
