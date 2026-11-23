---
name: new-domain-endpoint
description: This skill should be used when the user asks to "add a new API endpoint", "add a new resource", "expose a new field to the frontend", "add a new page setting", or wants a change that spans the database, the Go API, and the Vue frontend end-to-end. Sequences the full little-doc-editor stack for a new or changed REST resource.
---

# Adding a new REST resource end-to-end

little-doc-editor's REST resources span five layers that must stay in sync: Postgres schema → sqlc queries → Go chi handler → OpenAPI spec → generated TypeScript client → Pinia store → Vuetify UI. Skipping a layer is the most common source of drift in this codebase. This skill sequences all of them; use `add-migration` for step 1's details and consult the `db-migration-reviewer`, `go-backend-reviewer`, `api-contract-reviewer`, and `vue-frontend-reviewer` agents to check each layer once written.

## Sequence

1. **Schema.** If the resource needs new/changed columns or tables, write the migration (see the `add-migration` skill) and regenerate sqlc.
2. **Permission model.** Before writing the handler, decide which space role(s) can hit this endpoint (owner/admin/editor/viewer — space-level only, no per-page ACLs, SPEC.md Decision 7/24) and whether guests (via `GuestIdentity`) can reach it at all.
3. **Go handler.** Add the chi route, request/response structs, validation, and the space-role permission check. Use the `SearchIndexer`/`SearchQuerier` interface if the resource is searchable rather than writing ad hoc FTS SQL in the handler. Use `slog` for structured logging consistent with the rest of `api`.
4. **OpenAPI spec.** Add/update the endpoint definition — path, method, request/response schema, auth requirement, status codes — matching exactly what the handler reads/writes. This is the shared contract; treat drift between spec and handler as a bug, not a formatting nitpick.
5. **Regenerate the TypeScript client.** Run whatever generation command the repo uses against the updated spec (check for a script in `package.json` or a Makefile target) so the frontend gets typed request/response shapes.
6. **Pinia store.** Add the action/getter that calls the generated client method — avoid hand-written `fetch`/`axios` calls that bypass the generated types.
7. **Vuetify UI.** Wire the store into the component. Gate any action UI (buttons, menu items) on the same space-role logic as step 2 — this is UX, not the enforcement boundary, since the server must reject it too regardless of what the UI shows.
8. **i18n.** Route any new user-facing string through the `vue-i18n` catalog (Decision 13) rather than hardcoding it.
9. **Tests.** Add/update tests at whichever layers the repo currently tests (check existing test conventions rather than assuming a framework).

## Verification

After all layers are written, spawn (or ask the user to run):
- `db-migration-reviewer` if step 1 touched schema.
- `go-backend-reviewer` for the handler.
- `api-contract-reviewer` to check spec/handler/client agree.
- `vue-frontend-reviewer` for the UI layer.
- `security-reviewer` if the endpoint touches auth, sharing, or uploads.

## Common mistakes to avoid

- Adding a field to the Go struct and OpenAPI spec but forgetting to regenerate the TypeScript client, leaving the frontend typed against a stale contract.
- Enforcing a permission check only in the UI (hiding a button) without the matching server-side check in the handler.
- Writing raw SQL for a searchable field's query instead of going through the `SearchIndexer`/`SearchQuerier` interface, which then can't be swapped to Meilisearch/Typesense later (Decision 2) without a rewrite.
