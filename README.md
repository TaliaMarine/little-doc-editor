# little-doc-editor

A self-hosted, collaborative documentation and knowledge-base platform in the spirit of Notion or Confluence. Documents live in **spaces** as a tree of pages, are edited together in real time, and can be shared, exported and searched. One organisation per deployment; you run it yourself on Kubernetes with your own PostgreSQL, Redis and S3-compatible storage.

All screenshots below are generated from fictional demo data (see [DEVELOPER.md](./DEVELOPER.md#demo-data-and-screenshots)).

## Features

### Spaces and a page tree

Pages are organised into spaces and nest arbitrarily deep. The sidebar tree supports collapsing, drag-and-drop reordering and reparenting (plus a keyboard-friendly "Move to..." menu). Pages can be created blank or from a built-in template, renamed, locked against edits, and deleted. Each space has its own icon and history policy.

![Page tree and editor](docs/screenshots/01-page-tree-editor.png)

### Rich-text editor

A WYSIWYG editor (Tiptap/ProseMirror) with headings, lists, quotes, code blocks, links and inline images. Content is stored as ProseMirror JSON; Markdown and HTML are generated on demand for export.

### Live collaboration

Several people can edit the same page at once. Changes merge conflict-free (Yjs), and you see who else is on the page and where their cursor is. If the live connection is unavailable, the last saved version is shown read-only rather than silently dropping edits.

![Two users on the same page](docs/screenshots/02-live-collaboration.png)

### Attachments and a PDF viewer

Upload files to a page by dragging them onto it or choosing them. The server checks the real file type against an admin-configurable allowlist and size limit. Images can be inserted inline, and PDFs open in a built-in viewer. Files are fetched through the authenticated API, so permissions are checked on every view.

![PDF viewer](docs/screenshots/03-attachments-pdf-viewer.png)
![Inline image](docs/screenshots/04-inline-image-attachments.png)

### Diagrams with Mermaid

A code block with the language `mermaid` renders as a live diagram, with a toggle to edit its source.

![Mermaid diagram](docs/screenshots/05-mermaid-diagram.png)

### Roles, members and invites

Access is controlled per space with four roles: owner, admin, editor and viewer. Owners and admins manage members; editors change content; viewers read. Groups from your identity provider can be granted roles too. There is no open sign-up: an org admin creates an invite link (no email is sent, you share the link yourself), and the recipient sets a password. Org admins also manage users and instance-wide settings such as the attachment policy.

![Space members](docs/screenshots/06-space-members.png)
![Invites](docs/screenshots/07-admin-invites.png)

### Sharing with guest links

Any page can be shared by link, with view or edit permission, an optional password and an optional expiry. The link covers the page and its sub-pages. Guests need no account but must enter a display name, which is used for attribution. Links can be revoked at any time (which also ends guest sessions already issued from them), and editors can see an access log.

![Share dialog](docs/screenshots/08-share-link-dialog.png)
![Guest view](docs/screenshots/09-guest-share-view.png)

### Version history

Saving a page records revisions. Browse and preview earlier versions in the history drawer and restore one if you have edit rights. A scheduled in-process job prunes old revisions according to each space's maximum count and maximum age.

![History drawer](docs/screenshots/10-version-history.png)
![Version preview](docs/screenshots/12-version-preview.png)

### Export

Export a single page, a page with its sub-pages, or a whole space as Markdown or HTML (several pages arrive as a zip that mirrors the tree), or print a page to PDF from the browser.

![Export menu](docs/screenshots/11-export-menu.png)

### Search

Press **Ctrl/Cmd+K** (or the magnifier in the app bar) to search the current space. Full-text search runs on PostgreSQL FTS (`GET /api/v1/spaces/{id}/search`, up to 25 hits); results show page titles, navigate with the arrow keys and Enter. The API searches one space at a time and returns titles only, so there is no all-spaces scope or snippet yet. Search is hidden for share-link guests.

![Search dialog](docs/screenshots/13-search.png)

### Appearance

A theme menu in the app bar switches between System (follows the OS, the default), Light and Dark; the choice is saved in the browser. Mermaid diagrams follow the theme.

![Dark theme](docs/screenshots/14-dark-theme.png)

### Also built

- **Sign-in**: internal password accounts, plus a single OpenID Connect provider for single sign-on with just-in-time account creation, group-to-role mapping and an optional IdP-group-to-admin mapping.
- **Installable PWA** with an Android share target, so you can share a file from another app and attach it to a page.
- **Operations**: Prometheus metrics, optional OpenTelemetry tracing, rate limiting on the unauthenticated endpoints, and server-side logout.
- **Internationalisation groundwork**: all UI strings go through vue-i18n, but only English ships today.

## Deploying on Kubernetes

The supported way to run little-doc-editor is the Helm chart in [`charts/little-doc-editor/`](./charts/little-doc-editor/). It deploys three services as independent Deployments (`api`, `frontend`, `collab`) and a pre-install/pre-upgrade migration Job. `docker-compose.yml` in this repository is for local development only and must not be used to run it. The chart's [README](./charts/little-doc-editor/README.md) is the full values reference; this section is the short path to a working install.

### Prerequisites

- Kubernetes **1.36 or newer**.
- **PostgreSQL** you provide. The chart never bundles a database. The role used for migrations must be able to run `CREATE EXTENSION pg_trgm` (or create that extension beforehand).
- **Redis** you provide, used for collaboration fan-out and shared rate-limit counters. It needs `PUBLISH`/`SUBSCRIBE` if you restrict it with ACLs.
- **S3-compatible object storage** you provide (AWS S3, MinIO, GCS via its S3 API) with an existing bucket. For a tiny single-replica install the chart can instead keep attachments on a PersistentVolumeClaim (`storage.backend: filesystem`); see the chart README.
- An Ingress controller or a Gateway API implementation, with DNS names and TLS for the hosts below.
- Optional: an OpenID Connect provider, the Prometheus Operator, and the External Secrets Operator.

### Install

The chart is published as an OCI artifact; images default to the chart's `appVersion`.

```sh
helm install little-doc-editor oci://ghcr.io/taliamarine/charts/little-doc-editor \
  --version <chart-version> --namespace little-doc-editor --create-namespace -f values.yaml
```

A minimal `values.yaml`, with every secret read from a Kubernetes Secret you create beforehand (see [Secrets](#secrets)):

```yaml
postgres:
  dsn:
    existingSecret: { name: little-doc-editor-postgres, key: dsn }   # postgres://user:pass@host:5432/db?sslmode=require
redis:
  addr: redis.example.internal:6379
s3:
  endpoint: https://s3.example.internal   # omit for AWS S3
  bucket: little-doc-editor-attachments
  region: eu-west-1
  usePathStyle: true                      # false for AWS virtual-hosted style
  accessKey: { existingSecret: { name: little-doc-editor-s3, key: accessKey } }
  secretKey: { existingSecret: { name: little-doc-editor-s3, key: secretKey } }
sessionJWT:
  existingSecret: { name: little-doc-editor-session-jwt, key: secret }
bootstrapAdmin:
  email: admin@docs.example.com
  password: { existingSecret: { name: little-doc-editor-bootstrap-admin, key: password } }
ingress:
  enabled: true
  className: nginx
  frontend: { host: docs.example.com,        tls: { enabled: true, secretName: docs-tls } }
  api:      { host: api.docs.example.com,    tls: { enabled: true, secretName: docs-api-tls } }
  collab:   { host: collab.docs.example.com, tls: { enabled: true, secretName: docs-collab-tls } }
api:
  trustedProxies: ["10.0.0.0/8"]          # your ingress controller / gateway CIDR
```

Then open the frontend host and sign in as the bootstrap admin.

### Secrets

Every credential-shaped value (Postgres DSN, S3 keys, Redis password, OIDC client secret, the shared session-signing secret, the bootstrap admin password) accepts either an inline `value` or `existingSecret: {name, key}`. Prefer `existingSecret` in production. It composes with the External Secrets Operator, and [`docs/external-secrets-example.yaml`](./charts/little-doc-editor/docs/external-secrets-example.yaml) shows `ExternalSecret` manifests producing exactly the Secrets used above. `sessionJWT` is one value shared by `api` and `collab`, because `api` signs session tokens and `collab` validates the same ones; use a long random string.

### Exposing it: Ingress or Gateway API

`ingress.*` and `gatewayApi.*` are independent toggles (use either, or both). The chart gives the frontend, api and collab each their own host, and derives the SPA's runtime configuration (API and WebSocket URLs, Content-Security-Policy) and the api's and collab's allowed browser origins from those hosts, so the three-host layout above works without extra settings. Serving everything from one host is possible too (the SPA defaults to same-origin `/api/v1` and `/collab`), but the chart's Ingress/HTTPRoute templates are one host per service, so path routing would be your own manifest in front of the chart. The chart is vendor-neutral: pass controller-specific behaviour through `ingress.annotations` or your Gateway class, and have your controller or gateway redact the `/share/<token>` and `/invite/<token>` paths in its access logs, since those tokens are credentials.

### WebSockets and sticky sessions

Collaboration uses WebSockets to `collab`. Configure long idle/read timeouts on that route (for nginx, `proxy-read-timeout` and `proxy-send-timeout` of 3600). Each `collab` replica holds the live state of the documents it serves, so enable session affinity for the collab host in your ingress controller or Gateway class (for nginx, the cookie affinity annotation). The chart deliberately does not do this for you, because the setting is controller-specific.

### Sign-in, the first admin and break-glass

- **Bootstrap admin.** Set `bootstrapAdmin.email` and `bootstrapAdmin.password`. The account is created on api startup if it does not exist, and it is safe to leave set across upgrades. Sign in with it, then invite everyone else from the Invites screen.
- **OIDC (optional).** Set `oidc.enabled: true` with `oidc.issuer`, `oidc.clientID` and `oidc.redirectURL` (`https://<api host>/api/v1/auth/oidc/callback`, registered with your provider). Leave `oidc.clientSecret` empty to run as a public PKCE client. Accounts are created on first sign-in, but an IdP identity is never linked to an existing account by email. `oidc.adminGroup` maps an IdP group to org admin, `oidc.groupsClaim` names the groups claim, and `defaultMemberRole` (`viewer` or `editor`) gives every signed-in user an implicit role in all spaces. Admins can then disable password login instance-wide from the admin settings.
- **Break-glass.** If a misconfigured OIDC provider locks you out after password login was disabled, set `api.internalLoginBreakGlass: true` and upgrade. This re-enables password login for the bootstrap admin only (the chart refuses to render without `bootstrapAdmin.email`). Remove it again afterwards.

### Upgrades

`helm upgrade` runs database migrations first, as a pre-upgrade hook Job using the api image, before the new Deployments roll out. Migrations are forward-only in normal operation, so back up PostgreSQL before upgrading across versions. Pin the chart `--version`; images follow it unless you override `*.image.tag`.

### Before exposing it to users

Review these values (details in the chart README): `api.trustedProxies` (otherwise every client looks like the ingress to the rate limiter and share-link log), `api.rateLimit.*`, `api.historyCleanupSchedule`, `api.attachments.*`, and, if you scrape metrics, `observability.serviceMonitor.enabled`. Metrics are served on dedicated ports that are never routed through the ingress.

## Development

**Start here: [DEVELOPER.md](./DEVELOPER.md)** for local setup (Docker for Postgres/Redis/MinIO, running the three services, tests, regenerating demo data and screenshots). How the system fits together is in [ARCHITECTURE.md](./ARCHITECTURE.md); the full specification and decisions log is [SPEC.md](./SPEC.md), and open work is tracked in [NEXT.md](./NEXT.md). Each service has its own README: [api](./api/README.md), [frontend](./frontend/README.md), [collab](./collab/README.md), [Helm chart](./charts/little-doc-editor/README.md).

## AI-assisted development

This project's design and implementation make heavy use of AI assistance (Claude/Claude Code) — the specification, the Claude Code agents/skills configured in `.claude/`, and a substantial portion of the application code were drafted with an AI coding assistant and reviewed/directed by a human. If you're evaluating this codebase, expect AI-authored code throughout, reviewed against the decisions in SPEC.md rather than written from scratch line-by-line by hand.

## License

[MIT](./LICENSE)
