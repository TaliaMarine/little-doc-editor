import type { paths } from './schema'
import createClient, { type Middleware } from 'openapi-fetch'

// Auth state lives here (not imported from the auth store) so this module
// has no dependency on Pinia — the auth store calls setAuthToken/
// setUnauthorizedHandler instead of this file reaching into the store.
let authToken: string | null = null
let onUnauthorized: (() => void) | null = null

export function setAuthToken (token: string | null) {
  authToken = token
}

export function setUnauthorizedHandler (handler: (() => void) | null) {
  onUnauthorized = handler
}

const authMiddleware: Middleware = {
  async onRequest ({ request }) {
    if (authToken) {
      request.headers.set('Authorization', `Bearer ${authToken}`)
    }
    return request
  },
  async onResponse ({ response }) {
    if (response.status === 401) {
      onUnauthorized?.()
    }
    return response
  },
}

// The API has no CORS support (see DEVELOPER.md) — VITE_API_URL is only
// needed if frontend/api ever end up on different origins in production;
// locally the Vite dev server proxies /api to the api service instead.
export const api = createClient<paths>({
  baseUrl: import.meta.env.VITE_API_URL ?? '/api/v1',
})
api.use(authMiddleware)
