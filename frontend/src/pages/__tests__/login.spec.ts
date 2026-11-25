import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import Login from '@/pages/login.vue'
import i18nPlugin from '@/plugins/i18n'

const getOidcStatus = vi.fn()
vi.mock('@/api/generated', () => ({
  getOidcStatus: (...a: unknown[]) => getOidcStatus(...a),
  login: vi.fn(),
  logout: vi.fn(),
  getMe: vi.fn(),
}))
vi.mock('@/api/generated/client.gen', () => ({
  client: { setConfig: vi.fn(), interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } } },
}))
let query: Record<string, string> = {}
vi.mock('vue-router', () => ({ useRoute: () => ({ query }), useRouter: () => ({ push: vi.fn() }) }))

async function mountLogin () {
  const w = mount(Login, { global: { plugins: [createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] } })
  await flushPromises()
  return w
}

describe('login page SSO', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getOidcStatus.mockReset()
    query = {}
  })

  it('shows the SSO button only when OIDC is enabled', async () => {
    getOidcStatus.mockResolvedValue({ data: { enabled: false } })
    expect((await mountLogin()).text()).not.toContain('Sign in with SSO')
    getOidcStatus.mockResolvedValue({ data: { enabled: true } })
    expect((await mountLogin()).text()).toContain('Sign in with SSO')
  })

  it.each([
    ['email_taken', 'email address from your identity provider'],
    ['provider_error', 'identity provider reported an error'],
    ['weird', 'Single sign-on failed'],
  ])('maps sso_error reason %s to a message', async (reason, text) => {
    getOidcStatus.mockResolvedValue({ data: { enabled: true } })
    query = { sso_error: '1', reason }
    expect((await mountLogin()).text()).toContain(text)
  })
})
