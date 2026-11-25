import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAuthToken } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { RateLimitedError } from '@/utils/rateLimit'

const apiLogin = vi.fn()
const apiLogout = vi.fn()
vi.mock('@/api/generated', () => ({
  login: (...a: unknown[]) => apiLogin(...a),
  logout: (...a: unknown[]) => apiLogout(...a),
  getMe: vi.fn().mockResolvedValue({ data: undefined }),
}))
vi.mock('@/api/generated/client.gen', () => ({
  client: { setConfig: vi.fn(), interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } } },
}))

const jwt = `h.${btoa(JSON.stringify({ exp: 4_102_444_800 })).replaceAll('=', '')}.s`

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    apiLogin.mockReset()
    apiLogout.mockReset()
  })

  async function signedIn () {
    apiLogin.mockResolvedValue({ data: { token: jwt, displayName: 'Ada', isOrgAdmin: false }, response: { status: 200 } })
    const store = useAuthStore()
    await store.login('a@b.c', 'pw')
    return store
  }

  it('signOut revokes the token server-side and clears local state', async () => {
    const store = await signedIn()
    apiLogout.mockResolvedValue({})
    await store.signOut()
    expect(apiLogout).toHaveBeenCalledWith({ headers: { Authorization: `Bearer ${jwt}` } })
    expect(store.isAuthenticated).toBe(false)
    expect(getAuthToken()).toBeNull()
  })

  it('signOut still clears local state when the revoke call fails', async () => {
    const store = await signedIn()
    apiLogout.mockRejectedValue(new Error('offline'))
    await store.signOut()
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('little-doc-editor.session')).toBeNull()
  })

  it('login surfaces 429 with Retry-After', async () => {
    apiLogin.mockResolvedValue({ error: { error: 'slow down' }, response: { status: 429, headers: new Headers({ 'Retry-After': '42' }) } })
    const store = useAuthStore()
    await expect(store.login('a@b.c', 'pw')).rejects.toMatchObject({ retryAfter: 42 })
    await expect(store.login('a@b.c', 'pw')).rejects.toBeInstanceOf(RateLimitedError)
  })
})
