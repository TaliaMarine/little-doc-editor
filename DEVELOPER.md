# Developer guide

How to run little-doc-editor locally. See [SPEC.md](./SPEC.md) for what the system is supposed to do, and [ARCHITECTURE.md](./ARCHITECTURE.md) for how it's built (and [README.md](./README.md) for what it does and how to deploy it).

## Prerequisites

| Tool | Version | Check |
| --- | --- | --- |
| Go | 1.27+ | `go version` |
| Node.js | 24+ | `node --version` |
| Docker | any (this repo was validated against [Rancher Desktop](https://rancherdesktop.io/) with the moby/CE backend) | `docker info` |
| `just` (optional) | any | `just --version` |

Everything below has two forms — a `just` recipe and an equivalent `make` target — pick whichever you have. `just --list` / `make help`-style discovery: run `just` with no arguments to see every recipe.

If you use **Rancher Desktop** and `docker info` fails, the daemon likely isn't running yet:

```sh
open -a "Rancher Desktop"      # or launch it from Spotlight
# wait ~10-30s, then:
docker info                     # should succeed once the VM is up
```

## 1. Start local infrastructure

Postgres, Redis, and MinIO (S3-compatible storage) all run in Docker for local dev — none of them are bundled in production (see SPEC.md Decisions 18, 29 and §7.2). `docker-compose.yml` at the repo root defines all three, plus a one-shot `minio-init` job that creates the attachments bucket automatically.

```sh
just up      # or: make up   /   docker compose up -d
```

This starts:
- **Postgres 17** on `localhost:5432` (`postgres`/`postgres`, database `little_doc_editor`)
- **Redis 8** on `localhost:6379`
- **MinIO** on `localhost:9000` (API) / `localhost:9001` (console UI, login `minioadmin`/`minioadmin`), with a bucket named `little-doc-editor-attachments` already created

Check everything's healthy: `docker compose ps` — all three should show `(healthy)`.

Tear down: `just down` (keeps data) or `just reset` (wipes volumes and restarts clean — use this if migrations get into a bad state).

## 2. Run database migrations

```sh
just migrate      # or: make migrate
```

This runs `api/cmd/migrate` (this repo's own binary built on the golang-migrate library — no separate CLI install needed) against the Postgres container started in step 1.

## 3. Run the services

Each service runs in its own terminal (there's no single command that supervises all three — that's a deliberate simplicity choice for a 3-service monorepo; use whatever multi-terminal/tmux setup you like).

```sh
just api        # terminal 1 — Go API on :8080
just frontend   # terminal 2 — Vue dev server on :3000 (proxies /api to :8080)
just collab     # terminal 3 — Yjs/Hocuspocus WS server on :1234
```

`just api` sets `DEV_AUTH_BYPASS=true` — see the next section.

Verify the api is up: `curl localhost:8080/healthz` and `curl localhost:8080/readyz` (the latter checks the DB connection).

## Local auth: `DEV_AUTH_BYPASS`

For quick iteration, setting `DEV_AUTH_BYPASS=true` on the `api` service skips authentication entirely and treats **every request** as a fixed org-admin identity (`internal/authctx`). The `just api` / `make api` recipes already set this for you.

**Never set this outside your own machine.** It's purely a local-development affordance, logged loudly (a warning banner) every time the server starts with it enabled.

With it on, you can immediately do things like:

```sh
curl -X POST localhost:8080/api/v1/spaces \
  -H "Content-Type: application/json" \
  -d '{"name":"Engineering","slug":"engineering"}'
```

— no login step, and the request is authorized as if you were an org admin.

The **frontend** still needs a real session token to get past its login page (its router guard doesn't know about the bypass). Log in as **`dev-admin@localhost` / `dev`** — the bypass identity is seeded with that password on every bypass-mode startup, and **disabled** on every startup with the bypass off, so the well-known password never works outside bypass mode. `just api` also sets `SESSION_JWT_SECRET` to the same `dev-only-secret` that `just collab` uses, so that token is valid for collab connections too.

## Real auth, roles, and sharing

Real auth is implemented (internal login + OIDC) and is what actually enforces the space-level roles (owner/admin/editor/viewer) and share-link permissions — under `DEV_AUTH_BYPASS`, every request is an org admin, so it can't exercise any of that.

To run with real auth instead:

```sh
cd api
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/little_doc_editor?sslmode=disable"
export REDIS_ADDR=localhost:6379
export DEV_AUTH_BYPASS=false
export SESSION_JWT_SECRET=shared-dev-secret-change-me   # must match collab's SESSION_JWT_SECRET
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=correct-horse-battery-staple
export S3_ENDPOINT=http://localhost:9000 S3_BUCKET=little-doc-editor-attachments S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin S3_USE_PATH_STYLE=true
go run ./cmd/api
```

The bootstrap admin is created once, idempotently, on startup (Decision 11 — admin-invite-only signup needs a first account from somewhere). Then:

```sh
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -d '{"email":"admin@example.com","password":"correct-horse-battery-staple"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

SPACE_ID=$(curl -s -X POST localhost:8080/api/v1/spaces -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"Engineering","slug":"engineering"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
# the creator is automatically added as the space's owner

PAGE_ID=$(curl -s -X POST localhost:8080/api/v1/spaces/$SPACE_ID/pages -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Runbook","content":{"type":"doc","content":[]}}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

# Create a share link and redeem it anonymously (Decision 6: mandatory display name)
LINK_TOKEN=$(curl -s -X POST localhost:8080/api/v1/pages/$PAGE_ID/share-links -H "Authorization: Bearer $TOKEN" \
  -d '{"permission":"view"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
curl -s -X POST localhost:8080/api/v1/share/$LINK_TOKEN/redeem -d '{"displayName":"Anonymous Otter"}'
```

Admin settings — disabling internal login and the break-glass override (Decision 14):

```sh
curl -X PATCH localhost:8080/api/v1/admin/settings -H "Authorization: Bearer $TOKEN" -d '{"internalLoginDisabled":true}'
# Now login is blocked instance-wide. Restart the api process with
# INTERNAL_LOGIN_BREAK_GLASS=true to restore it regardless of that setting.
```

OIDC needs a real (or locally-run) identity provider to test against, which isn't part of this repo's docker-compose stack — set `OIDC_ENABLED=true` plus `OIDC_ISSUER`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET`/`OIDC_REDIRECT_URL` and visit `/api/v1/auth/oidc/login` once you have one.

## Testing attachment uploads

Attachment upload/download (`api/internal/attachments`) is the one feature with real end-to-end test coverage against live infrastructure (Postgres + MinIO), because a mocked S3 client wouldn't have caught either of the two real bugs found while building it (jsonb content serializing as base64 in JSON responses, and the AWS SDK's request-checksum handling needing a seekable body — both fixed, see git history).

```sh
just up               # Postgres + MinIO must be running
just migrate
just test-integration  # or: make test-integration
```

This exercises: create space → create page → upload (with a deliberately wrong client-supplied `Content-Type`, to prove the server sniffs real content instead of trusting it) → download → byte-for-byte comparison — plus the rejection paths (disallowed type, oversized file, upload to a nonexistent page).

The same `just test-integration` run also covers real auth, roles, and sharing (`api/internal/integrationtest/auth_sharing_test.go`): login success/failure/disabled-account, an authenticated-but-unauthorized user being rejected everywhere until promoted to a space role, and the full share-link create → resolve → redeem → descendant-scoped guest access → permission enforcement → revocation flow. These spin up their own real-auth server internally (not `DEV_AUTH_BYPASS`) — you don't need to set `DEV_AUTH_BYPASS=false` yourself to run them.

To try it by hand:

```sh
just api   # separate terminal

SPACE_ID=$(curl -s -X POST localhost:8080/api/v1/spaces \
  -d '{"name":"Test","slug":"test"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

PAGE_ID=$(curl -s -X POST localhost:8080/api/v1/spaces/$SPACE_ID/pages \
  -d '{"title":"Doc","content":{}}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

curl -X POST localhost:8080/api/v1/pages/$PAGE_ID/attachments -F "file=@/path/to/some/file.png"
```

The response's `content_type` is the *sniffed* type, not whatever the client sent. You can also browse uploaded objects directly in the MinIO console at `localhost:9001`.

## Demo data and screenshots

README screenshots are generated, not hand-taken. Two scripts (plain Node ESM, no build step; Node 24+) drive a real stack:

- `scripts/seed-demo/` fills a running api with **fictional** data **through the REST API only**: four users created via invite + redemption, one invite left pending, three spaces with members at different space roles, a nested page tree with realistic content (including a Mermaid block and a page with several revisions by different authors), a generated PNG (inline image) and PDF, and a view share link. It is safe to re-run: it looks up users by email, spaces by slug, pages by title under the same parent, and share links per page, and skips what exists, never overwriting page content edited since. To start over, drop and recreate the demo database.
- `scripts/screenshots/` (Playwright, Chromium) logs in as the seeded users via the API, injects the session, and writes `docs/screenshots/*.png` at a fixed 1440x900 viewport. The live-collaboration shot uses two browser contexts (an admin and a second user on the same page). It exits non-zero if the seed data is missing or a feature does not render.

Both need **real auth** (the bypass makes every request an admin, so roles cannot be shown), the api's rate limiting off (many logins in a minute), and `HISTORY_REVISION_DEBOUNCE=0` so the seed's saves become history entries. The `demo-*` recipes set all of that against a separate `lde_demo` database (override the name with `DEMO_DB=...`), so they never touch your dev data. Stop `just api` first (same port), then:

```sh
just up                  # Postgres, Redis, MinIO
just demo-migrate        # creates + migrates the lde_demo database
just demo-api            # terminal 1: api on :8080, real auth, admin@demo.example / demo-admin-password
just demo-collab         # terminal 2: collab on :1234
just frontend            # terminal 3: SPA on :3000
just demo-seed           # fictional data
just screenshots         # installs Playwright + Chromium on first use, then regenerates docs/screenshots/
```

(`make demo-migrate demo-api demo-collab demo-seed screenshots` are equivalent.) `just screenshots` shrinks the PNGs with `pngquant` when it is installed (`brew install pngquant`). Look at every PNG before committing: a login screen, blank page or error banner where a feature should be is a failure. Re-run `just screenshots` whenever the UI changes noticeably.

To aim the scripts at a stack on other ports or credentials, set `DEMO_API_URL` (default `http://localhost:8080/api/v1`, used by both scripts), `SHOT_BASE_URL` (the SPA, default `http://localhost:3000`), `DEMO_ADMIN_EMAIL`/`DEMO_ADMIN_PASSWORD` (the api's `BOOTSTRAP_ADMIN_*`) and `DEMO_USER_PASSWORD`. For example, a second stack on api `:18080` and the dev server started with `LDE_API_PROXY=http://localhost:18080 VITE_COLLAB_URL=ws://localhost:11234 npx vite --port 13000` uses `DEMO_API_URL=http://localhost:18080/api/v1 SHOT_BASE_URL=http://localhost:13000`.

Language screenshots are not taken because only English ships.

## Running the full test suite

```sh
just test              # api + frontend (Vitest) unit tests, no infra required (or: make test)
just test-integration  # requires `just up` + `just migrate` first
just lint              # go vet + frontend eslint/type-check + collab tsc
```

Per-service equivalents (if you'd rather work in one service's directory): see `api/README.md`, `frontend/README.md`, `collab/README.md`.

## Frontend runtime config and CSP

Production SPA config is runtime, not build-time: the nginx image's `docker/40-lde-config.sh` turns `API_BASE_URL` / `COLLAB_URL` env into `/config.js` and the CSP header. Test it locally with `docker build -t lde-fe frontend && docker run -p 8080:8080 -e API_BASE_URL=... lde-fe` and watch the browser console for CSP violations. In dev (`vite`) `public/config.js` is an empty stub and the `VITE_*` vars apply. See `frontend/README.md`.

## Helm chart

```sh
just chart-lint       # helm lint --strict
just chart-template   # helm template with default values
```

See `charts/little-doc-editor/README.md` for exercising the ESO/existingSecret paths and other optional features.

## Troubleshooting

**`docker: command not found` / `docker info` hangs** — the Docker CLI is installed but the daemon (Rancher Desktop, Docker Desktop, etc.) isn't running. Start the app and wait for it to finish booting its VM.

**Port already in use (5432/6379/9000/9001)** — something else on your machine is using that port. Either stop it, or edit `docker-compose.yml`'s port mappings (left side only, e.g. `"15432:5432"`) and update the corresponding `DATABASE_URL`/`REDIS_ADDR`/`S3_ENDPOINT` env vars to match.

**`migrate` fails with a permissions/extension error** — the `pg_trgm` extension requires `CREATE EXTENSION` privileges. The bundled `postgres` superuser has them; if you point at a different Postgres, make sure your role does too.

**api returns 401 on everything** — either you didn't set `DEV_AUTH_BYPASS=true` (`just api`/`make api` set it for you; set it yourself if running `go run ./cmd/api` directly), or you're intentionally running real auth and need to log in first and send `Authorization: Bearer <token>` — see "Real auth, roles, and sharing" above.

**api returns 403 "insufficient space role" / "insufficient access to this page"** — expected under real auth: the identity making the request isn't an org admin and has no (or an insufficient) role in that page's space. Add them as a member via `POST /api/v1/spaces/{spaceID}/members`, or use the org-admin token, or re-enable `DEV_AUTH_BYPASS=true` if you don't need to test role boundaries right now.

**Attachment upload returns 502** — usually means MinIO isn't reachable or the bucket doesn't exist. Check `docker compose ps` shows `minio` healthy and `minio-init` exited `0`: `docker compose logs minio-init`.
