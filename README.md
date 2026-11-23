# little-doc-editor

A self-hosted, collaborative documentation/knowledge-base platform (Notion/Confluence-like): Spaces containing hierarchical Pages, real-time collaborative WYSIWYG editing, sharing, search, versioning, and diagrams.

Full product/technical specification, including the resolved decisions log: [SPEC.md](./SPEC.md).

## AI-assisted development

This project's design and implementation make heavy use of AI assistance (Claude/Claude Code) — the specification, the Claude Code agents/skills configured in `.claude/`, and a substantial portion of the application code were drafted with an AI coding assistant and reviewed/directed by a human. If you're evaluating this codebase, expect AI-authored code throughout, reviewed against the decisions in SPEC.md rather than written from scratch line-by-line by hand.

## Architecture

Three independently deployable services, per SPEC.md §2 and Decision 12:

| Service    | Path         | Stack                                              | Purpose                                              |
| ---------- | ------------ | --------------------------------------------------- | ----------------------------------------------------- |
| `api`      | `api/`       | Go, chi, pgx/sqlc, go-oidc                          | REST API, auth, space/page CRUD, search, sharing      |
| `frontend` | `frontend/`  | Vue 3, Vuetify, Vite, TypeScript, Tiptap            | SPA — editor, navigation, admin UI                    |
| `collab`   | `collab/`    | Node/TypeScript, Hocuspocus (Yjs), Redis            | Real-time collaborative editing sidecar               |

Deployment: `charts/little-doc-editor/` — a Helm chart deploying all three services, expecting an externally provisioned PostgreSQL, S3-compatible object store, and Redis (bring-your-own only — see Decisions 18, 29).

## Status

The backend is functionally substantial and runs end-to-end against real local Postgres/Redis/MinIO (via `docker-compose.yml`); the frontend has login, the space/page tree nav, and a solo (non-collaborative) Tiptap editor with manual save. See SPEC.md for the full v1 scope and the Decisions Log for what's already settled — including one correction made during scaffolding (Decision 10: the assumed Vuetify `0.x` versioning reset never happened; verified against npm and corrected).

What exists so far:
- **api**: config loading, health/readiness endpoints, migrations through `000003` (domain model, `collab_documents`, `instance_settings`). **Real authentication** — argon2id internal login, OIDC authorization-code+PKCE with JIT provisioning, a bootstrap-admin startup step, instance-wide internal-login-disable with an env-var break-glass (Decision 14) — issuing the same HS256 session JWTs `collab` validates. **Space-level roles** (owner/admin/editor/viewer, Decision 7/24) enforced on every space/page/attachment/share-link endpoint. **Sharing** — opaque-token links, guest redemption with the mandatory display-name prompt (Decision 6), descendant-scoped access (Decision 21), an access log (Decision 16), revocation (which also ends guest sessions already issued from the link). **Page tree reordering.** **Search** over Postgres FTS behind a SearchIndexer/SearchQuerier boundary (Decision 2). **Attachment upload/download** with real content-type sniffing, an allowlist, and a size cap (Decision 17), now role-gated like everything else. All of this — not just attachments — is verified against real Postgres + MinIO in `internal/integrationtest`, not mocks.
- **frontend**: Vue 3 + Vuetify 4.1.12 + Pinia + vue-i18n + vue-router, a typed API client generated from `api/openapi.yaml`, internal-password login, the per-space page tree nav, and a solo Tiptap editor (StarterKit) saving via `PUT /pages/{id}/content`. Actions the caller can't perform are hidden using the `my_role`/`can_edit` fields the API reports (read-only editor for viewers, view-link guests, and locked pages). Vitest unit tests (`npm test`). Not yet wired to collab.
- **collab**: a working Hocuspocus WS server with auth-on-connect (shared-secret JWT — cross-verified against real tokens `api` issues), **per-document authorization** against the same space roles / share-link scopes as `api` (view-level access gets a read-only connection), Redis fan-out, and Postgres snapshot persistence.
- **charts/little-doc-editor**: Deployments/Services for all three, HPA/PDB, the migration hook Job, Ingress + Gateway API (independently toggled), a CRD-gated ServiceMonitor, and the inline-or-existingSecret pattern for every credential (including the api/collab-shared session-signing secret and the bootstrap-admin password) — passes `helm lint --strict` and `helm template` with all optional paths exercised.

What's not started: see [NEXT.md](./NEXT.md) — notably live collaboration in the editor, attachment UI, user invites/management, export, templates, history cleanup, metrics/tracing, and group-based space membership.

## Repository layout

```
api/                Go REST API service
frontend/           Vue 3 SPA
collab/             Yjs/Hocuspocus collaboration sidecar
charts/             Helm chart for deployment
docker-compose.yml  Local-only Postgres/Redis/MinIO for development (never used in production)
Justfile, Makefile  Root developer commands (up/migrate/test/lint/...) — equivalent, pick either
SPEC.md             Full specification and decisions log
NEXT.md             Authoritative AI-assisted-work TODO list — read this before "what's next?"
.claude/            Claude Code agents/skills tailored to this codebase
```

## Development

**Start here: [DEVELOPER.md](./DEVELOPER.md)** — local setup with Docker (Postgres/Redis/MinIO), running all three services, the `DEV_AUTH_BYPASS` local-auth shortcut, and testing attachment uploads end-to-end.

Each service also has its own README with service-specific detail:

- [api/README.md](./api/README.md)
- [frontend/README.md](./frontend/README.md)
- [collab/README.md](./collab/README.md)
- [charts/little-doc-editor/README.md](./charts/little-doc-editor/README.md)

## License

[MIT](./LICENSE)
