import { resolveApiBaseUrl, runtimeConfig } from '@/config'
import { client } from './generated/client.gen'

// Auth state lives here (not imported from the auth store) so this module
// has no dependency on Pinia — the auth store calls setAuthToken/
// setUnauthorizedHandler instead of this file reaching into the store.
let authToken: string | null = null
let onUnauthorized: (() => void) | null = null
// Share-link guest session (Decision 6/21). While set (only on the /share
// route) it replaces the member token for every request, so a signed-in
// member's own session is never sent as the guest and a guest 401 never
// signs the member out.
let guestToken: string | null = null
let onGuestUnauthorized: (() => void) | null = null

export function setAuthToken (token: string | null) {
  authToken = token
}

export function setGuestToken (token: string | null, onExpired: (() => void) | null = null) {
  guestToken = token
  onGuestUnauthorized = token ? onExpired : null
}

export function setUnauthorizedHandler (handler: (() => void) | null) {
  onUnauthorized = handler
}

// The API has no CORS support (see DEVELOPER.md) — VITE_API_URL is only
// needed if frontend/api ever end up on different origins in production;
// locally the Vite dev server proxies /api to the api service instead.
// Resolution: window.__LDE_CONFIG__.apiBaseUrl (container runtime config),
// then VITE_API_URL, then '/api/v1'.
const baseUrl = resolveApiBaseUrl(runtimeConfig(), import.meta.env.VITE_API_URL)
client.setConfig({ baseUrl })

// For the few hand-written requests (e.g. XHR upload with progress) that must
// match the generated client's base URL, token, and 401 handling.
export function getBaseUrl () {
  return baseUrl
}

export function getAuthToken () {
  return guestToken ?? authToken
}

// The token a request was sent with, so a late 401 is only attributed to the
// session that is still active (a guest link A response must not end link B).
export function currentSession (): { token: string | null, guest: boolean } {
  return { token: getAuthToken(), guest: guestToken !== null }
}

export function notifyUnauthorized (sent: { token: string | null, guest: boolean } = currentSession()) {
  if (sent.guest) {
    if (guestToken !== null && guestToken === sent.token) {
      onGuestUnauthorized?.()
    }
  } else if (!guestToken && authToken === sent.token) {
    onUnauthorized?.()
  }
}

// Which kind of token each request carried, so a 401 is attributed to the
// session that was actually sent (not whichever is active when it returns).
const sentWith = new WeakMap<Request, { token: string | null, guest: boolean }>()

client.interceptors.request.use(request => {
  const token = getAuthToken()
  sentWith.set(request, { token, guest: guestToken !== null })
  if (token) {
    request.headers.set('Authorization', `Bearer ${token}`)
  }
  return request
})
client.interceptors.response.use((response, request) => {
  // 401 from the public share endpoints means a wrong link password, not
  // an expired session.
  if (response.status === 401 && !new URL(response.url, window.location.origin).pathname.includes('/share/')) {
    // Attribute the 401 to the session that was actually sent.
    const sent = sentWith.get(request)
    if (sent) {
      notifyUnauthorized(sent)
    }
  }
  return response
})
