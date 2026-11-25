import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import AdminInvites from '@/pages/admin/invites.vue'
import AdminSettings from '@/pages/admin/settings.vue'
import AdminUsers from '@/pages/admin/users.vue'
import i18nPlugin from '@/plugins/i18n'
import { useAuthStore } from '@/stores/auth'

const api = vi.hoisted(() => ({
  listInvites: vi.fn(),
  createInvite: vi.fn(),
  revokeInvite: vi.fn(),
  listUsers: vi.fn(),
  setUserDisabled: vi.fn(),
  getAdminSettings: vi.fn(),
  updateAdminSettings: vi.fn(),
  getMe: vi.fn(),
  login: vi.fn(),
}))
vi.mock('@/api/generated', () => api)

const settings = {
  internal_login_disabled: false,
  updated_at: '',
  attachment_allowlist: ['image/png'],
  attachment_max_size_bytes: 10 * 1024 * 1024,
  attachment_policy_overridden: true,
  attachment_default_allowlist: ['image/png', 'application/pdf'],
  attachment_default_max_size_bytes: 25 * 1024 * 1024,
}

function mountPage (component: object, admin: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore()
  auth.isOrgAdmin = admin
  return mount(component, { global: { plugins: [pinia, createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] } })
}

describe('admin screens', () => {
  beforeEach(() => {
    // v-textarea (auto-grow) observes its size; jsdom has no ResizeObserver.
    vi.stubGlobal('ResizeObserver', class {
      observe () {} unobserve () {} disconnect () {}
    })
    for (const fn of Object.values(api)) {
      fn.mockReset()
    }
    api.getMe.mockResolvedValue({ data: undefined })
    api.listInvites.mockResolvedValue({ data: [] })
    api.listUsers.mockResolvedValue({ data: [] })
    api.getAdminSettings.mockResolvedValue({ data: settings })
  })

  it.each([
    ['invites', AdminInvites],
    ['users', AdminUsers],
    ['settings', AdminSettings],
  ])('%s: renders no controls and makes no requests for non-admins', async (_name, component) => {
    const w = mountPage(component, false)
    await flushPromises()
    expect(w.text()).toContain('You do not have access')
    expect(api.listInvites).not.toHaveBeenCalled()
    expect(api.listUsers).not.toHaveBeenCalled()
    expect(api.getAdminSettings).not.toHaveBeenCalled()
  })

  it('invites: lists and offers revoke only for pending invites', async () => {
    const base = { grants_org_admin: false, expires_at: null, redeemed_at: null, redeemed_by_user_id: null, revoked_at: null, created_by_user_id: 'u', created_by_display_name: 'Root', created_at: '' }
    api.listInvites.mockResolvedValue({ data: [
      { ...base, id: 'i1', email: 'a@x.test', status: 'pending' },
      { ...base, id: 'i2', email: null, status: 'revoked' },
    ] })
    const w = mountPage(AdminInvites, true)
    await flushPromises()
    expect(w.find('[data-testid="revoke-i1"]').exists()).toBe(true)
    expect(w.find('[data-testid="revoke-i2"]').exists()).toBe(false)
  })

  it('users: toggles disabled via the api', async () => {
    api.listUsers.mockResolvedValue({ data: [{ id: 'u1', email: 'b@x.test', display_name: 'Bee', auth_provider: 'internal', is_org_admin: false, disabled: false, created_at: '', last_login_at: null }] })
    api.setUserDisabled.mockResolvedValue({ data: {} })
    const w = mountPage(AdminUsers, true)
    await flushPromises()
    await w.find('[data-testid="toggle-u1"]').trigger('click')
    await flushPromises()
    expect(api.setUserDisabled).toHaveBeenCalledWith({ path: { userId: 'u1' }, body: { disabled: true } })
  })

  it('settings: saves the attachment policy and resets with nulls', async () => {
    api.updateAdminSettings.mockResolvedValue({ data: settings })
    const w = mountPage(AdminSettings, true)
    await flushPromises()
    await w.find('[data-testid="save-policy"]').trigger('click')
    await flushPromises()
    expect(api.updateAdminSettings).toHaveBeenCalledWith({ body: { attachmentAllowlist: ['image/png'], attachmentMaxSizeBytes: 10 * 1024 * 1024 } })
    await w.find('[data-testid="reset-policy"]').trigger('click')
    await flushPromises()
    expect(api.updateAdminSettings).toHaveBeenLastCalledWith({ body: { attachmentAllowlist: null, attachmentMaxSizeBytes: null } })
  })
})
