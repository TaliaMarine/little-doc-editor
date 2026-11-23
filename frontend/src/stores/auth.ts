import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, setAuthToken, setUnauthorizedHandler } from '@/api/client'

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
    setAuthToken(null)
    persist()
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
    const { data, error } = await api.POST('/auth/login', {
      body: { email, password },
    })
    if (error) {
      throw new Error(error.error ?? 'Login failed')
    }
    applySession(data)
  }

  // Completes an OIDC login: the api redirected back with the session token
  // in the URL fragment. isOrgAdmin isn't in the fragment, so ask /auth/me.
  async function loginWithToken (sessionToken: string, name: string) {
    applySession({ token: sessionToken, displayName: name, isOrgAdmin: false })
    const { data, error } = await api.GET('/auth/me')
    if (error || !data) {
      logout()
      throw new Error('Login failed')
    }
    applySession({ token: sessionToken, displayName: data.displayName, isOrgAdmin: data.isOrgAdmin })
  }

  // The api client has no store dependency of its own (see api/client.ts) —
  // it calls back into this store on a 401 instead of importing it.
  setUnauthorizedHandler(logout)
  restore()

  return { token, displayName, isOrgAdmin, isAuthenticated, login, loginWithToken, logout }
})
