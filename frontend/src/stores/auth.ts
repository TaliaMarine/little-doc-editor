import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { setAuthToken, setUnauthorizedHandler } from '@/api/client'
import { login as apiLogin, logout as apiLogout, getMe, type Me } from '@/api/generated'
import { attachmentBlobCache } from '@/utils/attachmentBlobCache'
import { RateLimitedError, retryAfterSeconds } from '@/utils/rateLimit'

const STORAGE_KEY = 'little-doc-editor.session'

interface StoredSession {
  token: string
  displayName: string
  isOrgAdmin: boolean
}

// The session token is a JWT; reading its exp claim (no verification — the
// server does that) lets restore() discard an already-expired session
// instead of "restoring" a login whose first request will 401.
function isExpired (token: string): boolean {
  try {
    const payload = JSON.parse(atob(token.split('.', 2)[1]!.replaceAll('-', '+').replaceAll('_', '/')))
    return typeof payload.exp !== 'number' || payload.exp * 1000 <= Date.now()
  } catch {
    return true
  }
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(null)
  const displayName = ref<string | null>(null)
  const isOrgAdmin = ref(false)
  const profile = ref<Me | null>(null)

  const isAuthenticated = computed(() => token.value !== null)

  function persist () {
    if (token.value && displayName.value !== null) {
      const stored: StoredSession = { token: token.value, displayName: displayName.value, isOrgAdmin: isOrgAdmin.value }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(stored))
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }

  function applySession (session: StoredSession) {
    token.value = session.token
    displayName.value = session.displayName
    isOrgAdmin.value = session.isOrgAdmin
    setAuthToken(session.token)
    persist()
  }

  function logout () {
    token.value = null
    displayName.value = null
    isOrgAdmin.value = false
    profile.value = null
    setAuthToken(null)
    persist()
    // Authenticated attachment bytes must not outlive the session (Decision 34).
    attachmentBlobCache.clear()
  }

  // Explicit sign-out: revoke the token server-side first (best effort — the
  // local session is cleared whether or not the call succeeds, e.g. offline or
  // already revoked). The 401 handler calls logout() directly instead.
  async function signOut () {
    const current = token.value
    // The bearer header is passed explicitly: the request interceptor reads
    // the module-level token lazily, which logout() below has already cleared.
    const revoke = current
      ? apiLogout({ headers: { Authorization: `Bearer ${current}` } }).catch(() => {})
      : Promise.resolve()
    logout()
    await revoke
  }

  function restore () {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) {
      return
    }
    try {
      const session = JSON.parse(raw) as StoredSession
      if (isExpired(session.token)) {
        localStorage.removeItem(STORAGE_KEY)
        return
      }
      applySession(session)
    } catch {
      localStorage.removeItem(STORAGE_KEY)
    }
  }

  async function login (email: string, password: string) {
    const { data, error, response } = await apiLogin({ body: { email, password } })
    if (response?.status === 429) {
      throw new RateLimitedError(retryAfterSeconds(response))
    }
    if (error) {
      throw new Error(error.error ?? 'Login failed')
    }
    applySession(data!)
    refreshProfile().catch(() => {})
  }

  // Loads the signed-in user's profile (email, username, avatar, IdP groups…).
  async function refreshProfile () {
    const { data } = await getMe()
    if (data) {
      profile.value = data
      isOrgAdmin.value = data.isOrgAdmin
      displayName.value = data.displayName
    }
  }

  // Completes an OIDC login: the api redirected back with the session token
  // in the URL fragment. isOrgAdmin isn't in the fragment, so ask /auth/me.
  async function loginWithToken (sessionToken: string, name: string) {
    applySession({ token: sessionToken, displayName: name, isOrgAdmin: false })
    const { data, error } = await getMe()
    if (error || !data || data.isGuest) {
      logout()
      throw new Error('Login failed')
    }
    applySession({ token: sessionToken, displayName: data.displayName, isOrgAdmin: data.isOrgAdmin })
    profile.value = data
  }

  // The api client has no store dependency of its own (see api/client.ts) —
  // it calls back into this store on a 401 instead of importing it.
  setUnauthorizedHandler(logout)
  restore()
  if (token.value) {
    refreshProfile().catch(() => {})
  }

  return { token, displayName, isOrgAdmin, isAuthenticated, profile, login, loginWithToken, logout, signOut, refreshProfile }
})
