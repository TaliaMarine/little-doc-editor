# NEXT.md

This file is the authoritative TODO list for AI-assisted work on little-doc-editor: it holds the
list of tasks needed to continue and finish the implementation (v1 scope per SPEC.md), in the
order they should be done.

**Convention: if this file lists a task, there is outstanding directed work to do.** When you
(the AI) pick this up in a fresh session with no other instruction, read this file and work the
first unblocked item top-to-bottom — items are ordered by dependency, not just priority. When you
finish an item, delete it (or move it to a "Done" note in your PR/commit description — don't leave
completed items lying around here) and update anything it unblocks. If a task turns out to be
already done, or the premise is stale, remove it and say why in your summary to the user rather
than silently skipping it. If this file is empty or says so explicitly, there's no standing
directed work — that's not an error, just meet whatever the user asks in the moment.

Background: SPEC.md is the product/technical spec and decisions log. DEVELOPER.md is the local
setup guide. README.md's "Status" section is a snapshot of what exists — it will drift from this
file over time; trust this file for what's *left*, README for what's *already built*.

**Ordering at a glance:** 1 (attachment UI) unblocks 3 (share target). 2 (accounts) has no code
dependencies but is what makes every role-based feature usable by more than one person, so it
comes before the remaining features. 7 (README/ARCHITECTURE docs with screenshots) waits for the
main UI features so the screenshots show the real product. 8 must be done before any real
(non-local) deployment, whatever else is finished.

**Caller capabilities are available for UI gating:** pages carry `my_role` + `can_edit`, spaces
carry `my_role` (see `api/openapi.yaml`). Use `frontend/src/utils/permissions.ts`
(`canEditPage`, `roleAtLeast`) to *hide* what the caller can't do, and add Vitest specs under
`src/**/__tests__/` (`npm test`; no jsdom/component mounting set up yet — add it with the first
component test).

---

## 1. Attachment upload/list UI (frontend)

Hide the upload controls unless `canEditPage(page)`.

Direct prerequisite for the PWA/Android Share Target work in §3. The backend is done and
integration-tested — upload, list (`GET /pages/{pageId}/attachments`), and download are all
implemented and documented in `api/openapi.yaml` (`uploadPageAttachment`, `listPageAttachments`,
`downloadAttachment`; client already regenerated). Uploads to locked pages are refused (403).

- Build an upload control in `frontend/src/components/PageEditor.vue` or a new
  `AttachmentPanel.vue` — file picker + drag-and-drop onto the editor, progress state, error
  surfacing for the rejection paths. Policy rejections are all **422** with the server's `error`
  message (too large / type not on the allowlist — see the `422` description in openapi.yaml);
  show it as-is, don't invent new copy.
- A list/gallery of a page's existing attachments via `listPageAttachments`.
- **Display/download approach is decided — SPEC.md Decision 34** (don't re-open it):
  `GET /attachments/{id}` needs the bearer token, so plain `<a href>`/`<img src>` can't load it.
  Fetch bytes via the api client → `URL.createObjectURL` → use the `blob:` URL for downloads
  (`<a download="{filename}">`), thumbnails, and the PDF viewer. Revoke object URLs on unmount;
  keep a small in-memory cache so revisiting a page doesn't refetch. No signed URLs.
- **Inline images store the attachment ID, not a URL** (Decision 34, SPEC.md §6.3) — a custom
  Tiptap image node/NodeView that resolves the ID to a blob URL at render time. This shape is
  baked into saved content and collab state, so get it right here; §4 (collab) and export will
  rely on it.
- PDF inline viewer (`pdf.js` / `vue-pdf-embed`, SPEC.md §5) can ride along here or be a follow-up
  — not required to unblock §3, just don't forget it's still open v1 scope.
- i18n every new string via `frontend/src/plugins/i18n.ts`'s catalog, not hardcoded.
- Spawn `api-contract-reviewer` and `vue-frontend-reviewer` before calling this done.

## 2. Accounts: invites, user management, members UI

Decision 11/15: signup is admin-invite-only with no SMTP. The `invites` table and
`api/db/queries/invites.sql` exist, but there are **no invite endpoints and no other way to create
an internal user** besides the bootstrap admin — a second internal account can only be made with
SQL. Until this exists, nothing role-based can be exercised by real people.

- Invites (org admin): create (token, optional email, optional `grants_org_admin`, expiry), list,
  revoke. Public: resolve + redeem (sets display name + password, argon2id via
  `internal/passwordhash`). Tokens are bearer credentials — same handling as share-link tokens
  (crypto/rand, never logged; `server.redactPath` already shows the pattern).
- Users (org admin): `GET /users` (needed to pick a user when adding a space member — the members
  endpoint takes a raw `userId`), disable/enable (`SetUserDisabled` already exists; the middleware
  and collab already enforce `disabled`).
- Frontend: invite admin screen, invite redemption route (public, like `/login`), space members
  screen (list/add/change role/remove — gated on `roleAtLeast(my_role, 'admin')`).
- Guard against removing/demoting a space's last owner while you're in `spacesapi`.
- Spawn `security-reviewer` (invite redemption is an account-creation path) and the usual
  backend/contract/frontend reviewers.

## 3. PWA + Android Share Target (SPEC.md §5.1, Decisions 31–33)

**Blocked on:** §1. Do not start this until attachment upload UI exists and works.

SPEC.md §5.1 has the design — read it before starting. Summary:
- `vite-plugin-pwa` in `frontend/` with the **`injectManifest`** strategy (a hand-written service
  worker — `generateSW` can't handle the share POST). Installability + share target only, **no**
  offline page-content caching (Decision 31).
- Manifest `share_target` entry (exact shape in SPEC.md §5.1), one file per share action
  (Decision 32 — matches the one-file-per-request upload endpoint, no backend change).
- The service worker intercepts `POST /share-target`, reads `formData()`, stashes the file(s) in
  the Cache API under a one-time key, and answers `303` → `GET /share-target?key=…` (Decision 33 —
  the static frontend server can't accept the POST itself).
- The `/share-target` SPA route reads the stash: authenticated → destination picker; no session →
  through `/login` (the router guard preserves `redirect=`), back to the picker. Clear the stash
  on success.
- Destination picker: reuse the tree nav data (`frontend/src/stores/pages.ts`,
  `PageTreeNav.vue`); only offer pages the caller can edit.
- Upload reuses the §1 upload call — don't duplicate it.
- Only testable on-device or via Chrome's manifest/share-target devtools emulation, not curl — say
  so explicitly if you can't verify it.

## 4. Live Yjs collaboration wiring (frontend ↔ collab service)

The `collab` service works end-to-end (Hocuspocus WS server, JWT auth, per-document authorization
re-checked on open connections, Redis fan-out, merge-on-write Postgres snapshots — see
`collab/README.md`). The frontend editor is solo-only with manual save via
`PUT /pages/{id}/content`.

- **Before adding the Yjs extension**, extract `PageEditor.vue`'s extensions list into a shared
  module (e.g. `frontend/src/editor/extensions.ts`) used by both solo and collab setups — a
  divergent list silently breaks sync (e.g. StarterKit's own history must be disabled when Yjs's
  undo is added). Do the extraction as part of this task, not before.
- Wire the Tiptap Yjs collaboration extension against collab's WS endpoint with the session JWT
  from `frontend/src/stores/auth.ts`. Use the canonical lowercase page ID as the document name —
  collab refuses anything else. Mirror read-only access in the UI via `can_edit` (`PageEditor.vue` already reacts to it; collab
  gives viewers/view-link guests a read-only connection and closes sockets whose access is
  revoked).
- **Seed new Yjs docs from existing content.** Pages created so far have content in
  `pages.content` and *no* `collab_documents` row; collab's `onLoadDocument` starts those empty,
  so the first live session would show (and persist) a blank document. Convert `pages.content` →
  Yjs on first load (`@tiptap/y-tiptap`'s `prosemirrorJSONToYDoc` with the shared schema) — in
  collab or on first client connect; decide which and note why.
- **Write live content back to `pages.content`.** Collab persists only opaque Yjs state, so
  `GET /pages/{id}`, search indexing, and future export go stale once editing moves to collab
  (SPEC.md §4 expects snapshots to feed them). Pick one owner for the Yjs → ProseMirror JSON
  conversion + `searchindex.IndexPage` call, and make sure manual `PUT /content` can't clobber
  live state.
- Decide what happens to the manual Save button (probably redundant for content; may matter for
  revisions, §9).
- **Collab tests**: there is no test runner. `access.ts`/`persistence.ts` were verified by hand
  against local Postgres; add a runner plus a WS-level test with `@hocuspocus/provider`.
- **Presence spoofing**: read-only connections can broadcast arbitrary awareness state; pin the
  user fields server-side to `context.displayName` once the frontend sets awareness.
- **Transient store failures aren't retried** (a failed `onStoreDocument` retries only on the
  next edit). Add backoff retry.
- Spawn `collab-sidecar-reviewer` and `vue-frontend-reviewer`.

## 5. Mermaid diagram node

**After §4** (so the node goes into the shared extensions module once). Follow the
`add-tiptap-node` skill (`.claude/skills/add-tiptap-node/SKILL.md`). SPEC.md §6.10 / §5: fenced
`mermaid` code block rendered live client-side via the `mermaid` npm package, no backend
rendering.

## 6. Page-tree nav: drag-and-drop reordering + collapsible groups

Show drag handles/"Move to…" only for `roleAtLeast(space.my_role, 'editor')`.

`PageTreeNav.vue` renders the tree flattened with indentation-by-depth, read-only, no
collapse/expand. The backend supports reordering (`PATCH /pages/{pageId}/move`) — undocumented in
`api/openapi.yaml`, not called from the frontend. Move requires **editor+ space role** (share-link
guests are refused even with an edit link) and a `parentId` in the **same space** (else 400).
- Add `movePage` to `api/openapi.yaml` and regenerate the client.
- Drag-and-drop reordering/reparenting, computing the fractional midpoint position client-side
  (the server persists whatever position it's given).
- Collapsible/expandable groups.
- A non-drag fallback ("Move to…" menu action) for keyboard/accessibility users.

## 7. User-facing docs: README with screenshots, ARCHITECTURE.md, Helm deployment guide

**After §1, §2, §4, §6** — the screenshots should show the actual product (attachments, members,
live collaboration, the page tree), not the current minimal UI. Re-run the screenshot step
whenever the UI changes noticeably; it must be scripted so that's cheap.

Today's `README.md` is developer-oriented (status snapshot, repo layout, links). Restructure the
docs so each has one audience:

- **`README.md` — what the app is and how to deploy it.**
  - Short description, then a features section (spaces/page tree, editor, live collaboration,
    attachments + PDF viewer, sharing with guest links, search, roles, i18n, PWA share target —
    only what's actually built at the time), each with a screenshot.
  - **Deployment: Kubernetes via the Helm chart only** (`charts/little-doc-editor/`). Cover
    prerequisites (Kubernetes 1.36+, bring-your-own PostgreSQL, Redis, S3-compatible storage —
    Decisions 5, 18, 29), the minimal `values.yaml`, secrets via inline values *or*
    existingSecret/ESO (Decision 19, link `docs/external-secrets-example.yaml`), Ingress vs
    Gateway API, WebSocket/sticky-session notes for collab (Decision 25), OIDC setup, bootstrap
    admin + break-glass (Decision 14), and upgrades (migrations run as a Helm hook). No
    docker-compose or bare-binary deployment instructions — `docker-compose.yml` is local-dev
    only. Summarize and link `charts/little-doc-editor/README.md` rather than duplicating its
    full values reference (two copies will drift).
  - Keep the "AI-assisted development" disclosure section and the license; move the status
    snapshot/repo layout out (to DEVELOPER.md or ARCHITECTURE.md) or drop what NEXT.md already
    covers.
- **`ARCHITECTURE.md` (new) — how it's built.** The three services and why (Decision 12), the
  diagram from SPEC.md §2 brought up to date, request/auth flow (session JWT shared by api and
  collab, share-link guest tokens re-validated per request/connection), collab data flow (Yjs,
  Redis fan-out, merge-on-write snapshots, how `pages.content` stays in sync once §4 lands),
  storage (Postgres schema overview, S3 attachments, Decision 34's blob-URL display), search,
  and repo layout. Link SPEC.md for the *why* of each decision rather than restating it.
- **Screenshots — produced by you (the AI), with dummy data, via a headless browser.**
  - A **repeatable demo-data seed** (e.g. `scripts/seed-demo` + a `just demo-seed` recipe) that
    creates obviously fictional spaces, nested pages with realistic-looking content, a couple of
    users with different roles, attachments (generated images/PDF), and a share link — through
    the REST API, not raw SQL, so it exercises the real paths. Never real names/emails/data.
  - A **screenshot script** using a headless browser — Playwright (Chromium) is the natural
    choice (`npx playwright install chromium`) — that logs in as the seeded users, visits each
    feature, and writes PNGs to `docs/screenshots/` at a fixed viewport (e.g. 1440×900) so
    re-runs produce stable diffs. Add a `just screenshots` recipe that assumes `just up`,
    `just migrate`, `just api`, `just frontend` (and `just collab`) are running, or starts them.
  - Live collaboration shots need two browser contexts (two users) on the same page so
    presence/cursors are visible.
  - Look at every generated image before referencing it in the README (blank pages, error
    states, or a login screen where a feature should be are failures, not screenshots).
  - Commit the PNGs (keep them reasonably small — PNG at the fixed viewport, no 4K).
- Update DEVELOPER.md with the seed + screenshot recipes, and README.md's "Development" pointer.

## 8. Required before any real deployment (security hardening)

Not features, but the gaps between "works locally" and "safe to expose" (SPEC.md §8):
- **Rate limiting.** Nothing is limited: share-link password redemption and `/auth/login` can be
  brute-forced at line rate, and each login/redeem costs an argon2id hash (CPU exhaustion), and
  each redeem writes rows. With multiple api replicas (Decision 3) an in-process limiter is
  per-replica only — decide between that and Redis-backed (Redis is already a dependency).
- **SPA CSP.** Attachment downloads send a restrictive CSP + `nosniff`; the SPA itself (frontend
  container) sends none.
- **Logout doesn't end the session server-side.** A user JWT stays valid until `SESSION_TTL`; the
  `sessions` table is written but never read. Add `revoked_at` checked by `authctx.Middleware`
  (and collab's per-message recheck).
- **Break-glass scope.** `INTERNAL_LOGIN_BREAK_GLASS` re-enables password login for *every*
  internal account; Decision 14 says "for the bootstrap admin". Restrict it.
- **OIDC JIT account linking.** If an internal user already has the IdP's email, `jitProvision`
  500s on the `users.email` UNIQUE constraint; `email_verified` isn't checked. Decide the policy
  deliberately (refuse with a clear error vs. link only on verified email).
- **Trusted proxies.** `hashIP` uses `r.RemoteAddr` — the ingress's address in production. Needs a
  trusted-proxy `X-Forwarded-For` config.

## 9. Remaining v1 scope (no design work done yet)

- **Page/space CRUD gaps**: no page delete (Decision 24: editors create/edit/**delete**), no
  lock/unlock toggle (`locked` is SQL-only), no space rename/settings update (incl. the history
  policy the cleanup job reads), no space delete. Collab already handles a page deleted while
  open (closes its connections).
- **Frontend OIDC login.** `GET /auth/oidc/callback` returns the session as JSON (no frontend
  callback route); the login page has no SSO button. Hand off without putting the token in a
  logged URL (fragment, or a one-time code exchanged via POST). Also: collab rejects an empty
  `displayName`, which an OIDC user with neither `name` nor `email` claims gets — fall back to
  something non-empty in `oidcapi`.
- **Share-link UI.** API side done (resolve/redeem with display-name prompt, Decision 6); no
  frontend `/share/:token` route, no UI to create/list/revoke links or view the access log
  (Decision 16). The access log itself is thin vs SPEC.md §6.6: only redemption is logged and
  `viewer_user_id` is never set.
- **Observability — Decision 27.** Neither api nor collab serves `/metrics`; nothing reads
  `OTEL_EXPORTER_OTLP_ENDPOINT` (the chart sets it). The chart's ServiceMonitor targets api only;
  add collab's `ws`-named port once collab exposes metrics.
- **Export** (PDF/Markdown/HTML) — Decision 13, §6.9.
- **Page templates** (blank, meeting notes, spec/RFC) — Decision 22, §6.9.
- **Revisions + history-lifecycle cleanup** — Decisions 9/28/30, §6.7. `CreatePageRevision`
  exists in `pages.sql` but nothing calls it, so revision creation (on save / collab debounce) is
  part of this, plus the in-process pruning job (logs only when it deletes) and a history UI
  (viewers browse, editors restore). Re-add a schedule Helm value wired to an env var the api
  actually parses (the old unused `api.historyCleanupSchedule` was removed).
- **Group-based space membership** — SPEC.md §3.1 `Group`; only direct user membership exists.
  `space_members.group_id` rows are ignored by both api's `GetSpaceMemberRole` and collab's
  `access.ts` — change both together.
- **Decision 17 admin-configurable attachment policy** — today env/Helm (`api.attachments.*`);
  move to `instance_settings` + adminapi.

## 10. Smaller cleanups (low priority, do opportunistically)

- **JSON casing inconsistency**: request bodies are camelCase (`parentId`, `displayName`),
  responses snake_case (`parent_id`, `created_at`). Frozen into the spec/client; new *response*
  fields follow snake_case for now. Pick one convention before many more endpoints land and record
  it in SPEC.md's Decisions Log.
- **`GetPageByID` errors are all reported as 404**, including real DB failures, across
  `pagesapi`, `attachments`, `shareapi`. Distinguish `pgx.ErrNoRows`.
- **collab Redis config**: no TLS / ACL-username options; `REDIS_ADDR` parsing via `split(':')`
  breaks on IPv6 literals.
- **`ListPagesBySpace` has no dedicated index** — add `(space_id, position, created_at, id)` via
  the `add-migration` skill if large spaces make it show up.
- **Frontend i18n catalog is a single inline object** (`frontend/src/plugins/i18n.ts`); split
  per-locale before adding a second locale.
