# frontend

Vue 3 SPA for little-doc-editor. Scaffolded with `create-vuetify` (Vue 3 + Vuetify + Vite + TypeScript + Pinia + vue-router + vue-i18n). See the repo-root [SPEC.md](../SPEC.md) §5 and [README.md](../README.md) for overall context.

## Current state

Scaffolding only: app shell (nav drawer + app bar), i18n message catalog, and an empty `spaces` Pinia store. No API integration, editor, or auth yet — the `api` service doesn't expose any domain endpoints yet either.

## Stack

Vue 3, Vuetify 4.1.12 (pinned — see SPEC.md Decision 10 for why this isn't a `0.x` version despite the original spec assumption), Vite, TypeScript, Pinia, vue-router, vue-i18n.

## Development

```sh
npm install
npm run dev          # dev server
npm run type-check   # vue-tsc
npm run lint         # eslint (eslint-config-vuetify)
npm run build         # production build
```

## Next steps

- Tiptap editor integration + Yjs collaboration extension (once `collab` and `api` expose the relevant endpoints).
- Typed API client generated from `api`'s OpenAPI spec, once that spec exists.
- Auth flow (OIDC redirect + internal login).
