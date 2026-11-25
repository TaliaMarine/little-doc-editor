import { defineConfig } from 'vitest/config'

// DB-backed tests need a migrated Postgres (api's migrations); point
// TEST_DATABASE_URL at a throwaway database, e.g.
//   createdb lde_collab && (cd ../api && DATABASE_URL=... go run ./cmd/migrate -direction up)
export default defineConfig({
  test: {
    include: ['test/**/*.test.ts'],
    fileParallelism: false,
    testTimeout: 20_000,
    env: {
      DATABASE_URL: process.env.TEST_DATABASE_URL ?? 'postgres://postgres:postgres@localhost:5432/lde_collab?sslmode=disable',
      REDIS_ADDR: process.env.TEST_REDIS_ADDR ?? 'localhost:6379',
      SESSION_JWT_SECRET: 'test-secret',
      STORE_RETRY_BASE_MS: '10',
      ACCESS_RECHECK_MS: '200',
    },
  },
})
