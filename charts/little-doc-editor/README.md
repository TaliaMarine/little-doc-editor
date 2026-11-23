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

Templates for the three Deployments/Services, HPA/PDB, the migration hook Job, Ingress, Gateway API HTTPRoute, and a conditional ServiceMonitor exist and pass `helm lint`/`helm template`. Not yet done: OpenTelemetry Collector wiring beyond the env var, and this hasn't been deployed against a real cluster.

## OIDC without a client secret

`oidc.clientSecret` is optional. Leave it empty to run as a **public client**: the api uses
authorization-code + PKCE and sends no secret. Register the redirect URL
(`https://<host>/api/v1/auth/oidc/callback`) with the identity provider as a public client. After login the
api redirects the browser to `/auth/callback` with the session in the URL fragment.

## Attachments on a PVC

`storage.backend=filesystem` stores attachments on a PVC (`api.persistence.*`) instead of S3. A
ReadWriteOnce volume supports one api replica (`api.replicaCount=1`; the Deployment uses `Recreate`).
