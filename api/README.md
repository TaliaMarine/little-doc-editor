# api

Go REST API service for little-doc-editor. See the repo-root [SPEC.md](../SPEC.md) for the full spec, [ARCHITECTURE.md](../ARCHITECTURE.md) for the overall architecture, and [DEVELOPER.md](../DEVELOPER.md) for the full local setup (docker-compose, all three services together).

## Stack

chi router, pgx/v5 (pgxpool), sqlc for type-safe queries, golang-migrate (via `cmd/migrate`, a small wrapper binary — no separate CLI needed at runtime) for schema migrations, stdlib `slog` for structured logging, AWS SDK v2 for S3-compatible attachment storage.

## Current state

- Config loading, health/readiness endpoints.
- Full initial domain-model migration (`db/migrations/000001_init_schema`), `collab_documents` (`000002`), and `instance_settings` (`000003`).
- **Real authentication** (`internal/authapi`, `internal/oidcapi`, `internal/passwordhash`, `internal/sessiontoken`): argon2id internal login, OIDC authorization-code+PKCE with JIT provisioning, a bootstrap-admin startup step, and an instance-wide internal-login-disable toggle with an env-var break-glass override (Decision 14). Session tokens are HS256 JWTs cross-verified against `collab`'s Node `jsonwebtoken` — same secret, same claim shape.
- **Space-level roles** (`internal/authz`, `internal/spacesapi`): owner/admin/editor/viewer membership, enforced on every space/page/attachment/share-link endpoint (Decision 7, 24). An org admin always passes.
- **Sharing** (`internal/shareapi`): opaque-token share links (view/edit permission, optional password/expiry), guest-identity redemption with the mandatory display-name prompt (Decision 6), descendant-scoped access (Decision 21), an access log visible to the page's editors and the space's admins/owners (Decision 16), and revocation.
- **Page tree reordering** (`pagesapi.move`): reparent/reposition with a cycle check.
- **Search** (`internal/searchindex`, `internal/searchapi`): Postgres FTS behind a SearchIndexer/SearchQuerier boundary (Decision 2) — plain text extracted from ProseMirror JSON on the Go side, `search_vector` updated on every page create/edit.
- **Attachment upload/download** (`internal/attachments`), enforcing Decision 17: real content-type sniffing via `mimetype` (not the client-sent header or filename), a configurable allowlist, and a size cap, now also gated by the same space-role checks as everything else.
- **Local dev auth bypass** (`internal/authctx`): set `DEV_AUTH_BYPASS=true` to skip authentication entirely and run every request as a fixed org-admin identity. **Never set this outside your own machine.**

All of the above is verified against real Postgres + MinIO in `internal/integrationtest`, not just unit-tested with mocks.

- **Accounts** (`internal/accountsapi`): admin-issued invites (hashed bearer tokens, single use, argon2id account on redemption), the org-admin user directory and disable/enable (Decisions 11, 15, 41). Space member management with a last-owner guard, including **group** principals fed by the IdP groups claim (Decision 40).
- **Page/space CRUD**: page delete, lock/unlock, space update/delete; built-in page templates (`/page-templates`, `createPage` with `templateKey`); revisions with a debounce rule, browse/restore, and the in-process cleanup job (`HISTORY_CLEANUP_SCHEDULE`) (Decisions 42, 43); server-side Markdown/HTML/print-HTML export (`internal/export`, `internal/exportapi`, Decision 44); the admin attachment policy in `instance_settings` (Decision 45).
- **Collab notification** (`internal/pagesync`): after committing a revision restore or a content-changing `PUT /pages/{id}/content`, the api publishes the page id (UUID string, nothing else) on the Redis channel `lde:page-content-replaced` (`pagesync.Channel`; collab's `PAGE_CONTENT_CHANNEL`) so collab merges the change into a live room immediately. Uses the `REDIS_*` settings (needs `PUBLISH` under ACLs); best-effort — a failure is logged as a warning and never fails the request (collab then picks the change up at its next store). No-op when `REDIS_ADDR` is unset.
- **Hardening** (Decisions 35-39): Redis-backed rate limiting (`internal/ratelimit`), trusted-proxy client IPs (`internal/clientip`), server-side logout (`sessions.revoked_at`), break-glass restricted to the bootstrap admin, a refuse-never-link OIDC account policy.
- **Observability** (Decision 27/47): Prometheus `/metrics` on `METRICS_ADDR`, OTLP tracing when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (`internal/telemetry`).

Not started: the frontend for most of the above (see the repo-root NEXT.md).

### Configuration added with these features

`REDIS_URL` (`redis://`/`rediss://`, same parsing as collab; wins over `REDIS_ADDR`), `REDIS_USERNAME`/`REDIS_PASSWORD`/`REDIS_TLS`, `CORS_ALLOWED_ORIGINS` (opt-in, exact origins, no credentials; Decision 48), `RATE_LIMIT_ENABLED`, `RATE_LIMIT_LOGIN_PER_MINUTE`, `RATE_LIMIT_PUBLIC_PER_MINUTE`, `TRUSTED_PROXIES`, `HISTORY_CLEANUP_SCHEDULE` (cron, `@every 6h`, or `off`), `HISTORY_REVISION_DEBOUNCE`, `METRICS_ADDR`, `OTEL_EXPORTER_OTLP_ENDPOINT`/`OTEL_EXPORTER_OTLP_PROTOCOL`. `INTERNAL_LOGIN_BREAK_GLASS` now requires `BOOTSTRAP_ADMIN_EMAIL`. All are validated at startup; see `internal/config` and `.env.example`.

## Local development

See [DEVELOPER.md](../DEVELOPER.md) for the full stack (docker-compose + all services). For just this service:

```sh
docker compose -f ../docker-compose.yml up -d   # Postgres, Redis, MinIO
cp .env.example .env
export $(grep -v '^#' .env | xargs)

make migrate-up          # runs cmd/migrate against DATABASE_URL
make run                  # starts the server on :8080
```

Check it's up: `curl localhost:8080/healthz` and `curl localhost:8080/readyz` (the latter checks the DB connection).

With `DEV_AUTH_BYPASS=true` (the `.env.example` default), every request runs as an org admin — no login step needed locally:

```sh
curl -X POST localhost:8080/api/v1/spaces -d '{"name":"Engineering","slug":"engineering"}'
```

To exercise real auth instead, set `DEV_AUTH_BYPASS=false` plus `SESSION_JWT_SECRET` and `BOOTSTRAP_ADMIN_EMAIL`/`BOOTSTRAP_ADMIN_PASSWORD` (see `.env.example`), then:

```sh
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -d '{"email":"admin@example.com","password":"correct-horse-battery-staple"}' | jq -r .token)
curl localhost:8080/api/v1/spaces -H "Authorization: Bearer $TOKEN"
```

## Testing

```sh
make test               # unit tests only, no infra required
make test-integration   # requires docker compose up -d first — see DEVELOPER.md
```

- `internal/attachments/validate_test.go`, `internal/passwordhash`, `internal/sessiontoken`, `internal/authctx` — unit tests, no infra.
- `internal/integrationtest` runs the full HTTP stack against real Postgres + MinIO:
  - `attachment_flow_test.go`: upload → download → byte-for-byte comparison, plus rejection paths.
  - `auth_sharing_test.go`: login (success/wrong-password/disabled-account), space-role enforcement (member/non-member/viewer-vs-editor), and the full share-link flow (create → resolve → redeem → descendant-scoped guest access → permission enforcement → revocation). These run against a real-auth server (not `DEV_AUTH_BYPASS`), since bypass mode makes every request an org admin regardless of what token is sent — see `newRealAuthTestServer`.

## Working with the schema

- Add a migration: `make migrate-create name=your_change`, then edit the generated `.up.sql`/`.down.sql` pair in `db/migrations/`. See the `add-migration` Claude Code skill (`.claude/skills/add-migration/`) for the rollout-safety conventions this project follows.
- After changing `db/queries/*.sql` or the schema, run `make sqlc` (requires `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`) to regenerate `internal/db/`.
