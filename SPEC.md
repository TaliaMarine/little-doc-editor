# little-doc-editor — Specification (DRAFT v0.3)

A self-hosted, collaborative documentation/knowledge-base platform (Notion/Confluence-like), with a Go backend and a Vuetify frontend.

Status legend:
- `[ASSUMPTION]` — a reasonable default; flag if wrong.
- `[OPEN]` — genuinely undecided, needs input (see §9).
- `[DECIDED]` — resolved in Q&A; kept inline for traceability.

## Decisions Log (resolved)
1. Collaboration engine: **Yjs + Node/Deno sidecar** (e.g. Hocuspocus-style); Go backend persists/relays opaque CRDT updates rather than implementing CRDT logic itself.
2. Search: **Postgres full-text search** (tsvector/pg_trgm), abstracted behind an interface for a future swap to Meilisearch/Typesense if needed.
3. Scaling: **multiple backend replicas expected in production** → Redis (or NATS) is in scope specifically for collab presence fan-out and shared pub/sub/session state. This is the deliberate exception to "avoid Redis unless needed."
4. Content source of truth: **ProseMirror/Tiptap JSON**; Markdown is generated on demand for export and search indexing (not the stored/versioned format).
5. Object storage: **S3-compatible only** (AWS S3 / MinIO / GCS via S3 API). No PVC-based attachment storage path — also fits multi-replica scaling more cleanly.
6. Anonymous edit-link access: **allowed**, gated by a mandatory display-name prompt before editing; that name is attached to their guest session for attribution in history and share-access logs.
7. Permission granularity: **space-level roles only** (owner/admin/editor/viewer). No per-page ACL overrides in v1.
8. Multi-tenancy: **single organization per deployment**.
9. History/versioning: **space-level lifecycle policy configurable by both max-count AND max-age** (whichever threshold is hit first prunes older revisions). Enforced by a scheduled cleanup job; the job logs a cleanup entry (what was deleted, when, how much) only when it actually deletes something — no-op runs are not logged.
10. Frontend framework: **Vue 3 + Vuetify**. `[CORRECTED]` The originally assumed "reset to `0.1.0+`" versioning change never happened — verified against the npm registry at scaffold time (2026-09-01): Vuetify continued conventional semver (`v3-stable: 3.13.2` → `latest: 4.1.12`, major version 4, no `0.x` release ever published). Scaffolded and pinned to `4.1.12`, the latest published stable at scaffold time.
11. Internal user signup: **admin-invite only**, no open self-service registration.
12. Helm topology: **split services** — `api` (Go REST + WS), `frontend` (static SPA), `collab` (Node/Deno Yjs sidecar) as independently scalable/upgradable Deployments.
13. V1 feature scope: **Export (PDF/Markdown/HTML)** and **page templates** are in. **Comments, in-app notifications, and transactional/notification email are deferred to v2**, along with **SCIM/LDAP provisioning, MFA, and a fully public/anonymous (no-login) space mode**. **UI i18n is in v1.**
14. Auth model is **OIDC-first**: internal (password) accounts are meant to be minimal — normally just the bootstrap admin. **Single OIDC issuer** configured instance-wide (not multi-provider) for v1. An instance-wide admin setting can **disable internal/password login entirely** once OIDC is configured, with a documented break-glass path (env var/CLI flag on the pod, not just DB state) to restore it for the bootstrap admin.
15. User invites: **no SMTP dependency in v1.** Admin generates an invite link/token in the UI and shares it out-of-band. SMTP is deferred alongside notifications.
16. Share-link access-log visibility: **space admins/owners + the page's editors**, not just space admins.
17. Attachments: **type allowlist + configurable max size**, both admin-configurable instance settings (default allowlist: images, PDF, common Office formats, zip, plain text/code).
18. Database: **always bring-your-own external PostgreSQL** — no bundled in-cluster Postgres subchart, even for dev/eval. Chart only ever consumes a connection secret.
19. OIDC + other secrets in Helm: values must support referencing an **existing Kubernetes Secret** (name/key) in addition to inline values, and be **External Secrets Operator (ESO) friendly** — the chart doesn't fight ESO-managed Secrets; docs show an example `ExternalSecret` producing the expected Secret shape.
20. Collab fan-out transport: **Redis**.
21. Share-link scope: sharing a page **includes its descendants by default** through the same link.
22. Page templates: **built-in only** for v1 (blank, meeting notes, spec/RFC, etc.); custom per-space templates deferred to v2.
23. Attachment AV/malware scanning: **out of scope for v1** — rely on type allowlist, size limits, and auth/sharing controls.
24. Space role capability split: **owner/admin** manage members, roles, and space settings; **editor** creates/edits/deletes content only (no member management); **viewer** is read-only.
25. Ingress/Gateway API chart docs: **vendor-neutral** — generic annotations map, no controller-specific defaults; sticky-session config documented as an operator responsibility for their chosen controller/Gateway class.
26. Minimum Kubernetes version target: **1.36+**.
27. Observability: **both Prometheus metrics and OpenTelemetry tracing** wired up in v1 (metrics endpoint + optional `ServiceMonitor`, OTLP exporter configurable via values).
28. History cleanup scheduler: runs **in-process** inside `api` on a configurable schedule — no separate Helm `CronJob` manifest.
29. Redis: **bring-your-own only**, consistent with Postgres — no bundled dev/eval subchart.
30. Viewer role can **see page revision history** (read-only browse), but cannot restore a version — that remains editor+.
31. **Installable PWA**: the frontend ships a web app manifest + service worker (`vite-plugin-pwa`), making it installable on Android/desktop. This is additive to the "no native app" stance (§1) — still one responsive web app, just installable; the service worker's job in v1 is installability + the share target below, **not** offline editing/caching of page content (out of scope for v1, revisit if requested).
32. **Android Share Target scope**: the manifest declares a `share_target` (`POST`, `multipart/form-data`) accepting **one file per share action**, matching the existing one-file-per-request attachment upload endpoint (`POST /api/v1/pages/{pageID}/attachments`) — no backend API change needed. If the OS share sheet lets the user multi-select files, the frontend uploads them sequentially, client-side, to the same destination page. Accepted file types are still authoritatively enforced server-side by the existing allowlist (Decision 17); the share-landing UI applies the same allowlist client-side only as an early UX check.
33. **Share landing flow**: the share target's `POST` is intercepted by the service worker (a SPA route can't read a POST body, and the static frontend server can't accept one), which **always** holds the shared file(s) in the browser Cache API keyed by a one-time token and redirects (`303`) to a dedicated SPA route. `[CORRECTED]` Originally the stash was described as happening only when there's no session; it has to happen on every share, since the SPA never sees the POST. If the user has no active session, they are sent through login, then returned to an "attach shared file" step to pick (or search) the destination space/page; the file then goes through the same attachment-upload call any in-app upload uses. On success, the frontend navigates to that page with the new attachment visible.
34. **Attachment display/download (decided 2026-10-31)**: the frontend fetches attachment bytes through the authenticated API client (`GET /api/v1/attachments/{id}` with the bearer token) and displays them from a tab-local `blob:` object URL — for downloads (`<a download>`), image thumbnails, inline images, and the PDF viewer. No signed/pre-authorized URLs and no direct S3 presigned URLs: permissions stay checked on every view (revocation is immediate), and no credential ever appears in a URL. Consequences: page content references inline images by **stable attachment ID** (never a fetchable URL, never a `blob:` URL), resolved at render time by a custom Tiptap image view; object URLs must be revoked on unmount; an in-memory cache avoids refetching on revisit. Accepted cost: whole-file loads with no streaming/range requests and no browser HTTP caching — acceptable under the 25MB default cap. Revisit (short-lived signed URLs) only if large PDFs or image-heavy pages make this a measured problem; that switch only changes the resolver, not stored content.

---

## 1. Goals & Non-Goals

**Goals**
- Self-hosted, single Helm-deployable stack.
- Spaces → hierarchical Pages, WYSIWYG editing backed by Markdown, real-time collaboration, sharing, search, versioning, diagrams.

**Non-goals**
- Not a generic project-management/ticketing tool (no kanban/tasks beyond maybe simple checklists inside a doc).
- Not multi-tenant SaaS — single organization per deployment (Decision 8).
- No mobile native app — responsive web only, though installable as a PWA with an Android Share Target for attaching shared files (Decision 31/32/33).
- No comments, in-app/email notifications, SCIM/LDAP, MFA, or public no-login spaces in v1 (Decision 13).

---

## 2. High-Level Architecture

```
┌─────────────┐      HTTPS/WS       ┌──────────────────┐      Yjs/WS      ┌────────────────┐
│  Vue/Vuetify │◄───────────────────►│   Go API server   │◄────────────────►│  collab sidecar │
│  SPA (Vite)  │                     │ (REST + auth +    │                  │ (Node/Deno,     │
└─────────────┘                     │  page/space CRUD)  │                  │  Yjs/Hocuspocus) │
                                     └────────┬───────────┘                  └────────┬─────────┘
                                              │                                        │
                    ┌─────────────────────────┼─────────────────────┬──────────────────┘
                    ▼                         ▼                     ▼
             ┌─────────────┐          ┌──────────────┐      ┌──────────────┐
             │ PostgreSQL  │          │ Object storage│      │ Redis / NATS │
             │ (external,  │          │ (S3-compatible)│     │ (collab      │
             │  BYO)       │          │ for attachments│     │  fan-out,    │
             │  metadata + │          │                │     │  presence)   │
             │  ProseMirror│          └──────────────┘      └──────────────┘
             │  JSON + FTS │
             └─────────────┘
```

- Three independently deployed services: `api`, `frontend`, `collab` (Decision 12).
- Redis/NATS is present specifically to fan out collab presence/updates across `api`/`collab` replicas (Decision 3).

---

## 3. Backend (Go — `api` service)

- Go 1.23+ (latest stable at build time).
- Suggested stack `[ASSUMPTION, swap freely]`:
  - HTTP: `chi` router.
  - DB access: `pgx` + `sqlc` (type-safe SQL, no ORM overhead).
  - Migrations: `golang-migrate`.
  - Auth: `go-oidc` (coreos) for the single OIDC issuer; local session/JWT handling for internal accounts (which can be instance-disabled per Decision 14).
  - WebSockets (presence/notifications channel, not the CRDT stream itself — that's the `collab` service): `nhooyr.io/websocket`.
  - Config: env vars + `.yaml`, validated at startup (12-factor, Helm-friendly), with first-class support for "value or secretRef" style config per Decision 19.
  - Structured logging: `slog` (stdlib).
  - Search: Postgres `tsvector` columns + GIN indexes, behind a `SearchIndexer`/`SearchQuerier` interface.
  - Scheduled jobs (history cleanup per Decision 9): in-process cron (e.g. `robfig/cron`) or a k8s `CronJob` hitting an internal admin endpoint `[OPEN-A]`.

### 3.1 Domain model (initial cut)
- `User` (internal or oidc-linked, `auth_provider` discriminator), `Group`, `Space`, `SpaceMember` (role), `Page` (tree via `parent_id`, `position`), `PageRevision`, `Attachment`, `ShareLink`, `ShareLinkAccessLog`, `Session`, `GuestIdentity` (for anonymous edit-link attribution), `Invite`, `HistoryCleanupLog`.

---

## 4. Collaboration sidecar (Node/Deno — `collab` service)

- Speaks the Yjs protocol (Hocuspocus or equivalent), holds the live CRDT document state in memory per active page, persists periodic snapshots + update log to Postgres (or a dedicated table set) so the `api` service can read consistent content for non-realtime consumers (search indexing, export, revision snapshots).
- Uses Redis/NATS pub/sub to broadcast updates across its own replicas and to `api` (Decision 3).
- Auth: validates the same session/JWT as `api` on WS connect (shared secret or JWKS check) before allowing a client to join a document room.

---

## 5. Frontend (`frontend` service)

- Vue 3 + Vuetify (pinned to `4.1.12` — see Decision 10's correction; there is no `0.x` line) + Vite + TypeScript.
- Editor: **Tiptap** (ProseMirror-based) — Markdown import/export extensions, image/file drop, code blocks with syntax highlighting, official Yjs collaboration extension talking to the `collab` service.
- Mermaid: `mermaid` npm package inside a custom Tiptap node (fenced ` ```mermaid ` block rendered live).
- PDF viewer: `pdf.js` (via `vue-pdf-embed` or similar), inline in the attachment panel.
- State/data: Pinia + typed API client generated from an OpenAPI spec `[ASSUMPTION]`.
- i18n: `vue-i18n`, extractable string catalog (Decision 13).
- Tree navigation (left sidebar): drag-and-drop reordering/reparenting, lazy-loaded per space.

### 5.1 Progressive Web App & Android Share Target

Depends on the editor/attachment UI existing first (this section describes the target design; build order is: attachment upload UI → this).

- **Build tooling**: `vite-plugin-pwa` with the `injectManifest` strategy (a hand-written service worker — `generateSW` can't intercept the share POST), generating the web app manifest. The service worker is for installability and the share-target POST only — no offline page-content caching in v1 (Decision 31).
- **Manifest `share_target`**:
  ```json
  {
    "share_target": {
      "action": "/share-target",
      "method": "POST",
      "enctype": "multipart/form-data",
      "params": {
        "title": "title",
        "text": "text",
        "files": [{ "name": "file", "accept": ["*/*"] }]
      }
    }
  }
  ```
  `accept` is intentionally broad in the manifest (Android/Chrome uses it only to decide which shares are offered to this app); the real allowlist check is the existing server-side one (Decision 17), applied when the upload actually posts to `/api/v1/pages/{pageID}/attachments`.
- **`POST /share-target`**: handled entirely by the service worker — read `formData()`, stash the file(s) in the Cache API under a one-time key, respond `303` → `GET /share-target?key=…`.
- **`/share-target` route**: a dedicated SPA route that reads the stash by key.
  - Authenticated session present → skip straight to the destination picker below.
  - No session → redirect to login, then resume at the picker on success (session cookie/token expiry mid-flow just re-triggers this path, it doesn't need special-casing).
- **Destination picker**: search/browse existing spaces & pages (reusing the tree nav's existing data), or create a new page inline; requires at least `editor` role on the chosen space (same enforcement as any other attachment upload — no new permission surface).
- **Upload**: one `POST /api/v1/pages/{pageID}/attachments` per shared file, sequential for multi-file shares (Decision 32); progress/failure per file, since a share can attach 1 file successfully and fail on another.
- **On completion**: navigate to the destination page with the new attachment(s) visible/highlighted; clear the Cache API stash entry.

---

## 6. Data Model Details

### 6.1 Spaces
- Top-level container: name, slug, icon, default page-history lifecycle policy (max-count / max-age, Decision 9), member list (users/groups + role).

### 6.2 Pages
- Tree structure (`parent_id`, sibling ordering).
- Content stored as ProseMirror/Tiptap JSON (Decision 4); Markdown generated on demand for export/search.
- Per-page settings: history enabled/disabled (inherits Space default, overridable), locked/read-only flag `[ASSUMPTION, optional]`, page template flag.

### 6.3 Attachments
- Images: pasted/dropped inline, stored in S3-compatible storage, referenced by **attachment ID** in content and resolved to a displayable URL at render time (Decision 34).
- Files: allowlisted types + configurable max size (Decision 17); PDFs get the inline viewer, others get a download link.

### 6.4 Users & Auth
- Internal users: email + password (argon2id), can be instance-disabled once OIDC is set up (Decision 14).
- OIDC users: linked via `sub` + issuer, JIT-provisioned on first login, optional group-claim → internal-group mapping.
- Both types share the `users` table with an `auth_provider` discriminator.
- Invites: admin-generated token/link, no email dependency (Decision 15).

### 6.5 Permissions
- Space-level roles (Decision 24): `owner`/`admin` manage members, roles, and space settings; `editor` creates/edits/deletes pages only; `viewer` is read-only. No per-page ACLs in v1 (Decision 7).
- Org-level: `admin` (manage users/spaces/instance settings) vs regular `member`.

### 6.6 Sharing
- `ShareLink`: opaque random token (not the page's real ID), scope = a page **and its descendants by default** (Decision 21), permission = `view` or `edit`, optional expiry, optional password.
- Anonymous edit access requires a display-name prompt (Decision 6); attribution goes through a lightweight `GuestIdentity`.
- `ShareLinkAccessLog`: timestamp, hashed IP, user-agent, and viewer identity if authenticated/guest-named. Visible to space admins/owners + the page's editors (Decision 16).

### 6.7 History / Versioning
- Immutable `PageRevision` rows (author or guest identity, timestamp, snapshot/diff), created on save or on a debounce interval during live collaborative sessions (not every keystroke).
- Space-level lifecycle: prune by max-count and max-age, whichever fires first (Decision 9); an in-process scheduled job (Decision 28) logs only actual deletions.
- Restore-to-version UI; `viewer` role can browse history but not restore (Decision 30).

### 6.8 Search
- Postgres FTS (`tsvector` + GIN, `pg_trgm` for fuzzy matches) over generated Markdown/plaintext extracted from page JSON (Decision 2).

### 6.9 Export & Templates
- Export a page (with descendants, optionally) or a whole space to PDF, Markdown, or HTML (Decision 13).
- Page templates: fixed built-in set (blank, meeting notes, spec/RFC, etc.) for v1; custom per-space templates deferred to v2 (Decision 22).

### 6.10 Diagrams
- Mermaid: rendered client-side via `mermaid.js` from fenced code blocks; no backend-side rendering needed.

---

## 7. Infrastructure / Deployment

### 7.1 Database
- External PostgreSQL only (Decision 18), latest stable (16/17). Migrations run via a Helm pre-upgrade/pre-install hook Job.

### 7.2 Object storage
- S3-compatible only (Decision 5): endpoint, bucket, region, access key / IRSA-style auth all configurable via values + secret refs.

### 7.3 Redis
- Redis (Decision 20), used specifically for collab fan-out and shared session/presence state across `api`/`collab` replicas (Decision 3). **Bring-your-own only** (Decision 29) — no bundled subchart, same pattern as Postgres.

### 7.4 Helm chart
- Three Deployments: `api`, `frontend`, `collab`, each with their own replica count, resources, HPA, and PodDisruptionBudget values.
- Target minimum Kubernetes version: **1.36+** (Decision 26) — Gateway API resources can be assumed broadly available at stable/beta.
- Secrets: every credential-shaped value (Postgres DSN, S3 keys, OIDC client secret, Redis auth) supports **inline value OR existing-Secret name/key reference**, ESO-friendly (Decision 19). Example `ExternalSecret` manifests included in chart docs.
- Ingress **and** Gateway API support: `values.yaml` toggles (`ingress.enabled` / `gatewayApi.enabled`) each generating the appropriate resource (`Ingress` vs `HTTPRoute` + reference to an existing `Gateway`), independently for `frontend` vs `api`/`collab` routes (the latter need WS-upgrade-friendly routing/timeouts called out explicitly in values comments). Kept vendor-neutral: generic annotations map, no controller-specific defaults (Decision 25).
- Session affinity: since `collab` holds in-memory CRDT room state per replica, the chart documents that sticky-session config is the operator's responsibility for their chosen ingress controller/Gateway class (Decision 25), with a comment pointing at where to add the relevant annotation/policy.
- No bundled Postgres subchart (Decision 18); values only ever reference an external DB connection.
- Observability: `api`/`collab` expose a `/metrics` endpoint; optional `ServiceMonitor` resource created if the Prometheus Operator CRD is present; OpenTelemetry tracing instrumented with a values-configurable OTLP exporter endpoint (Decision 27).

---

## 8. Security
- Password hashing: argon2id (for the internal accounts that remain, per Decision 14).
- OIDC: standard authorization-code + PKCE flow, ID token validation, JIT provisioning, optional group-claim → internal-group mapping.
- Share links: cryptographically random tokens, rate-limited access, optional password + expiry, revocable, access-tracked (Decision 16).
- CSRF/XSS: sanitize pasted HTML → ProseMirror JSON on paste; CSP headers.
- File upload: content-type sniffing + allowlist + size limits (Decision 17). No AV scanning in v1 (Decision 23).

---

## 9. Remaining Open Questions

None — all questions raised during spec drafting are resolved; see the Decisions Log at the top. Anything that surfaces during implementation will be added here.
