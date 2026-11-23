import { client } from './generated/client.gen'

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

// The API has no CORS support (see DEVELOPER.md) — VITE_API_URL is only
// needed if frontend/api ever end up on different origins in production;
// locally the Vite dev server proxies /api to the api service instead.
client.setConfig({ baseUrl: import.meta.env.VITE_API_URL ?? '/api/v1' })

client.interceptors.request.use(request => {
  if (authToken) {
    request.headers.set('Authorization', `Bearer ${authToken}`)
  }
  return request
})
client.interceptors.response.use(response => {
  if (response.status === 401) {
    onUnauthorized?.()
  }
  return response
})
