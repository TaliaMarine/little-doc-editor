# Root developer entry point — see DEVELOPER.md for the full walkthrough.
# Mirrors Justfile 1:1 for contributors who prefer `make` over `just`.
#
# The env vars below match docker-compose.yml's defaults exactly. Override
# per-service by copying that service's .env.example to .env instead.

.PHONY: up down reset migrate migrate-down api frontend collab generate-api test test-integration lint chart-lint chart-template demo-migrate demo-api demo-collab demo-seed screenshots

DATABASE_URL := postgres://postgres:postgres@localhost:5432/little_doc_editor?sslmode=disable
REDIS_ADDR := localhost:6379
# Shared by `api` and `collab` — collab validates the tokens api signs.
SESSION_SECRET := dev-only-secret
S3_ENV := S3_ENDPOINT=http://localhost:9000 S3_BUCKET=little-doc-editor-attachments S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin S3_USE_PATH_STYLE=true

up:
	docker compose up -d

down:
	docker compose down

reset:
	docker compose down -v
	$(MAKE) up

migrate:
	cd api && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate -direction up

migrate-down:
	cd api && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate -direction down

# Runs the api service with DEV_AUTH_BYPASS=true — every request is treated
# as an org admin. NEVER set this outside your own machine. The frontend
# logs in as dev-admin@localhost / dev (see DEVELOPER.md).
api:
	cd api && DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" DEV_AUTH_BYPASS=true SESSION_JWT_SECRET="$(SESSION_SECRET)" $(S3_ENV) go run ./cmd/api

frontend:
	cd frontend && npm run dev

collab:
	cd collab && DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" SESSION_JWT_SECRET="$(SESSION_SECRET)" npm run dev

# Regenerate the frontend's typed API client from api/openapi.yaml. Run
# after changing openapi.yaml or the handlers it describes.
generate-api:
	cd frontend && npm run generate:api

test:
	cd api && go test ./...
	cd frontend && npm test

# Requires `make up` (and `make migrate`) first.
test-integration:
	cd api && DATABASE_URL="$(DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" DEV_AUTH_BYPASS=true $(S3_ENV) go test -tags integration ./internal/integrationtest/... -v

lint:
	cd api && go vet ./...
	cd frontend && npm run lint
	cd frontend && npm run type-check
	cd collab && npm run typecheck

chart-lint:
	helm lint --strict charts/little-doc-editor

chart-template:
	helm template test charts/little-doc-editor

# --- Demo data + README screenshots (see DEVELOPER.md) ---
# Override with DEMO_DB=<name> to use another database.
DEMO_DB ?= lde_demo
DEMO_DATABASE_URL := postgres://postgres:postgres@localhost:5432/$(DEMO_DB)?sslmode=disable

demo-migrate:
	docker compose exec -T postgres psql -U postgres -tc "SELECT 1 FROM pg_database WHERE datname='$(DEMO_DB)'" | grep -q 1 || docker compose exec -T postgres psql -U postgres -c "CREATE DATABASE $(DEMO_DB)"
	cd api && DATABASE_URL="$(DEMO_DATABASE_URL)" go run ./cmd/migrate -direction up

demo-api:
	cd api && DATABASE_URL="$(DEMO_DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" DEV_AUTH_BYPASS=false SESSION_JWT_SECRET="$(SESSION_SECRET)" BOOTSTRAP_ADMIN_EMAIL=admin@demo.example BOOTSTRAP_ADMIN_PASSWORD=demo-admin-password RATE_LIMIT_ENABLED=false HISTORY_REVISION_DEBOUNCE=0 $(S3_ENV) go run ./cmd/api

demo-collab:
	cd collab && DATABASE_URL="$(DEMO_DATABASE_URL)" REDIS_ADDR="$(REDIS_ADDR)" SESSION_JWT_SECRET="$(SESSION_SECRET)" npm run dev

demo-seed:
	node scripts/seed-demo/seed.mjs

screenshots:
	cd scripts/screenshots && npm install --no-audit --no-fund && npx playwright install chromium
	node scripts/screenshots/shoot.mjs
