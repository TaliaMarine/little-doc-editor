# little-doc-editor Helm chart

Deploys the three little-doc-editor services (`api`, `frontend`, `collab`) as independent Deployments, per SPEC.md §7.4 and Decision 12. See the repo-root [SPEC.md](../../SPEC.md).

## Requirements

- Kubernetes 1.36+
- An external, bring-your-own PostgreSQL (Decision 18), Redis (Decision 29), and S3-compatible object store (Decision 5) — this chart bundles none of them.

## Secrets

Every credential-shaped value in `values.yaml` (Postgres DSN, S3 keys, OIDC client secret, Redis auth, the shared `sessionJWT` signing secret, the bootstrap admin password) supports either an inline `value` or a reference to an existing Kubernetes Secret via `existingSecret.{name,key}` (Decision 19). For production, prefer `existingSecret` populated by [External Secrets Operator](https://external-secrets.io/) — see [docs/external-secrets-example.yaml](docs/external-secrets-example.yaml) for the expected shape.

`sessionJWT` is shared between `api` and `collab` — both must resolve to the exact same secret (SPEC.md §4: `api` issues session tokens, `collab` validates the same ones on WS connect), which is why it's one top-level value rather than a per-service field.

## First admin account

With `oidc.enabled: false` (or before OIDC is configured), set `bootstrapAdmin.email` and `bootstrapAdmin.password` to create the first internal org-admin account on startup — a no-op once any org admin already exists, so it's safe to leave set across upgrades (Decision 11: admin-invite-only signup means someone has to send the first invite).

## Ingress vs Gateway API

`ingress.enabled` and `gatewayApi.enabled` are independent toggles, each configurable per-service. The `api` and `collab` routes carry WebSocket traffic and typically need longer idle timeouts than `frontend` — see the comments in `values.yaml` under `ingress.api` / `gatewayApi.api`.

## Hardening and operations values (api)

- `api.trustedProxies` — CIDRs/IPs of your ingress/gateway whose `X-Forwarded-For` is believed. **Set this in production**, otherwise rate limiting and the share-link access log see every client as the ingress.
- `api.corsAllowedOrigins` — origins allowed to call the api cross-origin (`CORS_ALLOWED_ORIGINS`; exact origins, never `*`, no credentials/cookies). Defaults to the frontend origin(s) derived from `ingress.frontend.host` / `gatewayApi.frontend.hostnames` when the api host differs from the frontend host; off for same-origin setups.
- `collab.allowedOrigins` — origins allowed to open the collab WebSocket (`COLLAB_ALLOWED_ORIGINS`; other browser origins get 403). Defaults to the derived frontend origin(s).
- `api.rateLimit.*` — login/redeem limits per client IP; counters live in Redis (`redis.*`), falling back to per-replica memory if Redis is unreachable.
- `api.historyCleanupSchedule` / `api.revisionDebounce` — the in-process history cleanup job (cron, `@every 6h`, or `off`; one replica runs each tick) and the revision debounce.
- `api.internalLoginBreakGlass` — restores password login for the **bootstrap admin only**; the chart fails to render unless `bootstrapAdmin.email` is also set.
- `api.metrics.*` — Prometheus `/metrics` on a dedicated `metrics` Service port (never on the ingress); `observability.serviceMonitor.enabled` scrapes it plus collab's `/metrics` on its `metrics` port.
- `observability.otel.{endpoint,protocol}` — OTLP trace export (`http/protobuf` on 4318 or `grpc` on 4317).
- `redis.username` / `redis.tls` apply to both api and collab. No new credentials were introduced (the api reuses `redis.auth`), so `docs/external-secrets-example.yaml` is unchanged.

## Sticky sessions

`collab` holds in-memory Yjs document state per replica. This chart does not configure session affinity itself — add the relevant annotation/policy for your ingress controller or Gateway class (see the `collab.stickySessionsNote` comment in `values.yaml`).

## Install

```sh
helm install little-doc-editor . \
  --set postgres.dsn.existingSecret.name=little-doc-editor-postgres \
  --set postgres.dsn.existingSecret.key=dsn \
  --set redis.addr=my-redis:6379 \
  ...
```

## Current state

Templates for the three Deployments/Services, HPA/PDB, the migration hook Job, Ingress, Gateway API HTTPRoute, and a conditional ServiceMonitor exist and pass `helm lint`/`helm template`. Not yet done: this hasn't been deployed against a real cluster.

## Frontend runtime config / CSP

The frontend image generates `/config.js` and its Content-Security-Policy (`connect-src`, `img-src`) at start from `API_BASE_URL` and `COLLAB_URL`. The chart sets these on the frontend Deployment: `frontend.config.apiBaseUrl` / `frontend.config.collabUrl` if set, otherwise derived from `ingress.{api,collab}.host` (scheme from that host's `tls.enabled`) or `gatewayApi.{api,collab}.hostnames` (https/wss assumed), otherwise unset (same-origin `/api/v1`, `/collab`). A separate api origin is cross-origin for the SPA, so the api's opt-in CORS is wired too (see below).

## OIDC without a client secret

`oidc.clientSecret` is optional. Leave it empty to run as a **public client**: the api uses
authorization-code + PKCE and sends no secret. Register the redirect URL
(`https://<host>/api/v1/auth/oidc/callback`) with the identity provider as a public client. After login the
api redirects the browser to `/auth/callback` with the session in the URL fragment.

## Attachments on a PVC

`storage.backend=filesystem` stores attachments on a PVC (`api.persistence.*`) instead of S3. A
ReadWriteOnce volume supports one api replica (`api.replicaCount=1`; the Deployment uses `Recreate`).
