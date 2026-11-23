.PHONY: run build test test-integration lint sqlc migrate-up migrate-down migrate-create

run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api

test:
	go test ./...

# Requires docker compose up -d (Postgres + MinIO) and DEV_AUTH_BYPASS=true —
# see ../DEVELOPER.md. Excluded from `make test` by the integration build tag.
test-integration:
	go test -tags integration ./internal/integrationtest/... -v

lint:
	go vet ./...

sqlc:
	sqlc generate

# DATABASE_URL must be set, e.g.
#   export DATABASE_URL=postgres://postgres:postgres@localhost:5432/little_doc_editor?sslmode=disable
# Uses cmd/migrate (this repo's own binary, built on the golang-migrate
# library) rather than requiring the separate migrate CLI — same binary the
# Helm chart's migration hook Job runs in production.
migrate-up:
	go run ./cmd/migrate -direction up

migrate-down:
	go run ./cmd/migrate -direction down

# Requires the migrate CLI (go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest) —
# only needed for scaffolding new migration file pairs, not for applying them.
# Usage: make migrate-create name=add_something
migrate-create:
	migrate create -ext sql -dir db/migrations -seq $(name)
