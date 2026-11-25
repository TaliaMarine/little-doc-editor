# Root developer entry point — see DEVELOPER.md for the full walkthrough.
# Mirrors Makefile 1:1 for contributors who prefer `just` over `make`.
#
# The env vars below match docker-compose.yml's defaults exactly. Override
# per-service by copying that service's .env.example to .env instead.

set shell := ["bash", "-euo", "pipefail", "-c"]

database_url := "postgres://postgres:postgres@localhost:5432/little_doc_editor?sslmode=disable"
redis_addr := "localhost:6379"
# Shared by `api` and `collab` — collab validates the tokens api signs.
session_secret := "dev-only-secret"
s3_env := "S3_ENDPOINT=http://localhost:9000 S3_BUCKET=little-doc-editor-attachments S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin S3_USE_PATH_STYLE=true"

default:
    @just --list

# --- Local infrastructure (Postgres, Redis, MinIO) ---

up:
    docker compose up -d

down:
    docker compose down

# Wipe all local dev data (Postgres + MinIO volumes) and start clean.
reset:
    docker compose down -v
    just up

# --- Database ---

migrate:
    cd api && DATABASE_URL="{{database_url}}" go run ./cmd/migrate -direction up

migrate-down:
    cd api && DATABASE_URL="{{database_url}}" go run ./cmd/migrate -direction down

# --- Run services (each in its own terminal — see DEVELOPER.md) ---

# Runs the api service with DEV_AUTH_BYPASS=true — every request is treated
# as an org admin. NEVER set this outside your own machine. The frontend
# logs in as dev-admin@localhost / dev (see DEVELOPER.md).
api:
    cd api && DATABASE_URL="{{database_url}}" REDIS_ADDR="{{redis_addr}}" DEV_AUTH_BYPASS=true SESSION_JWT_SECRET="{{session_secret}}" {{s3_env}} go run ./cmd/api

frontend:
    cd frontend && npm run dev

collab:
    cd collab && DATABASE_URL="{{database_url}}" REDIS_ADDR="{{redis_addr}}" SESSION_JWT_SECRET="{{session_secret}}" npm run dev

# --- API contract ---

# Regenerate the frontend's typed API client from api/openapi.yaml. Run
# after changing openapi.yaml or the handlers it describes.
generate-api:
    cd frontend && npm run generate:api

# --- Tests ---

# Unit tests only — no docker/infra required.
test:
    cd api && go test ./...
    cd frontend && npm test

# Requires `just up` (and `just migrate`) first.
test-integration:
    cd api && DATABASE_URL="{{database_url}}" REDIS_ADDR="{{redis_addr}}" DEV_AUTH_BYPASS=true {{s3_env}} go test -tags integration ./internal/integrationtest/... -v

lint:
    cd api && go vet ./...
    cd frontend && npm run lint
    cd frontend && npm run type-check
    cd collab && npm run typecheck

# --- Demo data + README screenshots (see DEVELOPER.md "Demo data and screenshots") ---
# A separate database (lde_demo) and real auth, so none of this touches your dev data
# and the seeded users/roles are enforced for real.

# Override with DEMO_DB=<name> to use another database (e.g. a second checkout's stack).
demo_db := env_var_or_default("DEMO_DB", "lde_demo")
demo_database_url := "postgres://postgres:postgres@localhost:5432/" + demo_db + "?sslmode=disable"

# Create the lde_demo database (if missing) and migrate it. Needs `just up`.
demo-migrate:
    docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='{{demo_db}}'" | grep -q 1 || docker compose exec -T postgres psql -U postgres -c "CREATE DATABASE {{demo_db}}"
    cd api && DATABASE_URL="{{demo_database_url}}" go run ./cmd/migrate -direction up

# api against lde_demo with real auth (bootstrap admin admin@demo.example), history debounce and rate limiting off.
demo-api:
    cd api && DATABASE_URL="{{demo_database_url}}" REDIS_ADDR="{{redis_addr}}" DEV_AUTH_BYPASS=false SESSION_JWT_SECRET="{{session_secret}}" BOOTSTRAP_ADMIN_EMAIL=admin@demo.example BOOTSTRAP_ADMIN_PASSWORD=demo-admin-password RATE_LIMIT_ENABLED=false HISTORY_REVISION_DEBOUNCE=0 {{s3_env}} go run ./cmd/api

demo-collab:
    cd collab && DATABASE_URL="{{demo_database_url}}" REDIS_ADDR="{{redis_addr}}" SESSION_JWT_SECRET="{{session_secret}}" npm run dev

# Fill the running demo-api with fictional spaces, pages, users, attachments and a share link. Safe to re-run.
demo-seed:
    node scripts/seed-demo/seed.mjs

# Regenerate docs/screenshots/*.png (needs `just up`, demo-migrate, demo-api, demo-collab, frontend running, then demo-seed).
screenshots:
    cd scripts/screenshots && npm install --no-audit --no-fund && npx playwright install chromium
    node scripts/screenshots/shoot.mjs

# --- Helm chart ---

chart-lint:
    helm lint --strict charts/little-doc-editor

chart-template:
    helm template test charts/little-doc-editor
