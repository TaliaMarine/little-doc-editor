import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import AdminInvites from '@/pages/admin/invites.vue'
import InviteRedeem from '@/pages/invite.vue'
import i18nPlugin from '@/plugins/i18n'
import { useAuthStore } from '@/stores/auth'

const api = vi.hoisted(() => ({
  resolveInvite: vi.fn(),
  redeemInvite: vi.fn(),
  listInvites: vi.fn(),
  createInvite: vi.fn(),
  revokeInvite: vi.fn(),
  getMe: vi.fn(),
  login: vi.fn(),
}))
vi.mock('@/api/generated', () => api)
const replace = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ replace, push: vi.fn() }) }))

function mountWith (component: object, props: object = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return { pinia, w: mount(component, {
    props,
    global: {
      plugins: [pinia, createVuetify(), i18nPlugin as ReturnType<typeof createI18n>],
      stubs: { RouterLink: true },
    },
  }) }
}

describe('invite redemption page', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) {
      fn.mockReset()
    }
    replace.mockReset()
  })

  it('explains a used/expired invite (410)', async () => {
    api.resolveInvite.mockResolvedValue({ error: { error: 'gone' }, response: { status: 410 } })
    const { w } = mountWith(InviteRedeem, { token: 'tok' })
    await flushPromises()
    expect(w.text()).toContain('already used')
    expect(w.find('input[type="password"]').exists()).toBe(false)
  })

  it('locks the email when the invite is bound and signs in on success', async () => {
    api.resolveInvite.mockResolvedValue({ data: { email: 'bound@x.test', expiresAt: null }, response: { status: 200 } })
    api.redeemInvite.mockResolvedValue({ data: { token: 'jwt', displayName: 'Bo', isOrgAdmin: false }, response: { status: 201 } })
    api.getMe.mockResolvedValue({ data: { displayName: 'Bo', isOrgAdmin: false, isGuest: false } })
    const { w } = mountWith(InviteRedeem, { token: 'tok' })
    await flushPromises()
    const inputs = w.findAll('input')
    expect(inputs[0]!.element.value).toBe('bound@x.test')
    expect(inputs[0]!.attributes('readonly')).toBeDefined()
    await inputs[1]!.setValue('Bo')
    await inputs[2]!.setValue('correct-horse-battery')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(api.redeemInvite).toHaveBeenCalledWith({ path: { token: 'tok' }, body: { displayName: 'Bo', password: 'correct-horse-battery' } })
    expect(useAuthStore().isAuthenticated).toBe(true)
    expect(replace).toHaveBeenCalledWith('/')
  })

  it('shows the server message when the email is taken (409)', async () => {
    api.resolveInvite.mockResolvedValue({ data: { email: null, expiresAt: null }, response: { status: 200 } })
    api.redeemInvite.mockResolvedValue({ error: { error: 'an account with this email already exists' }, response: { status: 409 } })
    const { w } = mountWith(InviteRedeem, { token: 'tok' })
    await flushPromises()
    const inputs = w.findAll('input')
    await inputs[0]!.setValue('dup@x.test')
    await inputs[1]!.setValue('Dup')
    await inputs[2]!.setValue('correct-horse-battery')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('an account with this email already exists')
    expect(replace).not.toHaveBeenCalled()
  })
})

describe('invite creation', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('shows the link once and forgets the token when the dialog closes', async () => {
    vi.stubGlobal('ResizeObserver', class {
      observe () {} unobserve () {} disconnect () {}
    })
    vi.stubGlobal('visualViewport', { width: 1024, height: 768, offsetLeft: 0, offsetTop: 0, scale: 1, addEventListener () {}, removeEventListener () {} })
    api.listInvites.mockResolvedValue({ data: [] })
    api.createInvite.mockResolvedValue({ data: { id: 'i', token: 'SECRET-TOKEN' } })
    const pinia = createPinia()
    setActivePinia(pinia)
    useAuthStore().isOrgAdmin = true
    const w = mount(AdminInvites, { attachTo: document.body, global: { plugins: [pinia, createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] } })
    await flushPromises()
    await w.find('[data-testid="create-invite"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector('[data-testid="submit-invite"]') as HTMLElement).click()
    await flushPromises()
    const field = document.body.querySelector('[data-testid="invite-link"] input') as HTMLInputElement
    expect(field.value).toBe(`${location.origin}/invite/SECRET-TOKEN`)
    expect(api.createInvite.mock.calls[0]![0].body.expiresAt).toBeTypeOf('string')
    ;(document.body.querySelector('[data-testid="close-invite"]') as HTMLElement).click()
    await flushPromises()
    expect(document.body.innerHTML).not.toContain('SECRET-TOKEN')
    w.unmount()
  })
})
