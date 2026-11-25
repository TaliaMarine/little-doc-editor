# frontend

Vue 3 SPA for little-doc-editor. Scaffolded with `create-vuetify` (Vue 3 + Vuetify + Vite + TypeScript + Pinia + vue-router + vue-i18n). See the repo-root [SPEC.md](../SPEC.md) §5 and [ARCHITECTURE.md](../ARCHITECTURE.md) for overall context.

## Current state

Auth, spaces/page tree, the live collaborative editor, attachments, Mermaid diagrams, and the PWA/Android share target are implemented. Editor code lives in `src/editor/`:

- `extensions.ts` — the single extension list (`createExtensions`); every editor (live collab, read-only fallback, tests) uses it. It must stay in sync with `collab/src/schema.ts`. Add new nodes here *and* there.
- `AttachmentImage.ts` — inline node storing only an `attachmentId` (never a URL); the NodeView resolves it to an authenticated `blob:` URL (`utils/attachmentBlobCache.ts`, SPEC Decision 34).
- `MermaidCodeBlock.ts` — Mermaid is a `codeBlock` with `language: 'mermaid'` (no separate node); the NodeView renders a lazy-loaded, `securityLevel: 'strict'` preview.
- `collabSession.ts` / `composables/useCollabSession.ts` — Hocuspocus provider, status, presence. No manual Save for content; if collab can't connect the last saved content is shown read-only.
- `pages/share-link.vue` + `stores/guest.ts` — public `/share/:token[/:pageId]` guest route: mandatory display-name prompt (Decision 6), password, then the page via the redeemed guest token (`api/client.ts setGuestToken` makes it the active api/collab token only while on that route). Guests never get history, the share dialog or the space tree.
- `PageTreeNav.vue` + `utils/pageMove.ts`/`treeCollapse.ts` — collapsible tree (per-user localStorage), drag-and-drop reorder/reparent with client-side fractional positions, a "Move to…" menu for keyboard users, and a `menu-items` slot for per-page actions.
- `ShareLinkDialog.vue` / `PageHistoryDrawer.vue` — member-only share-link management (+ access log) and revision history (restore is editor+).
- `sw/sw.ts` + `utils/shareStash.ts` — service worker for installability and the Android share-target POST (SPEC §5.1).

Accounts and space management (all gated via `utils/permissions.ts` / `utils/accounts.ts`; the api enforces regardless): org-admin screens under `/admin/{invites,users,settings}` (invite link shown once; attachment policy; internal-login toggle), public `/invite/:token` redemption, per-space `/spaces/:id/members` (owner/admin) and `/spaces/:id/settings` (rename/icon/history policy, owner-only delete), page actions (`PageActions.vue`: lock, delete, sub-page from template) and export (`ExportMenu.vue`; zips are fetched as authenticated blobs, PDF = returned print HTML loaded into a hidden sandboxed iframe and `print()`ed). Message strings for these live in `src/locales/accounts.ts`.

## Configuration

| Variable | Meaning |
| --- | --- |
| `VITE_API_URL` | api base URL (default `/api/v1`, same-origin) |
| `VITE_COLLAB_URL` | collab WebSocket URL; default `ws://localhost:1234` in dev, same-origin `/collab` (ws/wss) in production builds |
| `LDE_API_PROXY` | dev-server only: api proxy target (default `http://localhost:8080`) |

### Runtime config and CSP (container)

The image is static, so API/collab URLs are injected at container start rather than build time. `docker/40-lde-config.sh` (nginx `/docker-entrypoint.d`) reads:

| Env (container) | Meaning |
| --- | --- |
| `API_BASE_URL` | `/path` or `http(s)://host[:port][/path]`; default `/api/v1` |
| `COLLAB_URL` | `/path` or `ws(s)://host[:port][/path]`; default same-origin `/collab` |

and writes `/config.js` (`window.__LDE_CONFIG__`, loaded by `index.html` before the app, served `no-store`; the service worker caches nothing) plus the security headers. Precedence in the SPA: `window.__LDE_CONFIG__`, then `VITE_*`, then defaults. Invalid values make the container refuse to start.

Every response carries `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` (share/invite tokens live in the URL path) and `X-Frame-Options: DENY`. CSP: `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data: <api origin>; font-src 'self' data:; connect-src 'self' blob: <api origin> <collab origin>; worker-src 'self' blob:; frame-src about: blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'`. Relaxations: `style-src 'unsafe-inline'` (Vuetify inline styles, print iframe), `img-src blob: data:` (authenticated attachment blobs), `connect-src blob:` (pdf.js reads the attachment blob URL), `worker-src blob:` / `frame-src about: blob:` (defensive; print iframe is srcdoc). pdf.js's worker is a normal hashed same-origin asset (`PdfViewer.vue` uses `vue-pdf-embed`'s essential build, since the default build inlines the worker as a `data:` URL which the CSP rightly blocks); nginx maps `.mjs` to `text/javascript` for it. nginx does not access-log `/share/*` and `/invite/*` (the token is a bearer credential in the path); ingress-controller/gateway access logs still will, so redact those paths there. The `index.html` `<meta name="referrer">` is kept as a fallback for `vite preview`/dev, which don't run nginx.

The service worker is only registered in production builds; test the PWA with `npm run build && npm run preview`.

## Stack

Vue 3, Vuetify 4, Vite, TypeScript 6, Pinia, vue-router, vue-i18n, Tiptap 3 + Yjs/Hocuspocus, mermaid, vue-pdf-embed, vite-plugin-pwa (injectManifest). Vitest runs under jsdom with `@vue/test-utils`.

## Development

```sh
npm install
npm run dev          # dev server
npm run type-check   # vue-tsc
npm run lint         # eslint (eslint-config-vuetify)
npm run build         # production build
npm test             # vitest (jsdom)
```
