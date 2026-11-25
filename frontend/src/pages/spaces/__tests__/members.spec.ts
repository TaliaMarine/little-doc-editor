import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import SpaceMembers from '@/pages/spaces/members.vue'
import i18nPlugin from '@/plugins/i18n'
import { useSpacesStore } from '@/stores/spaces'

const api = vi.hoisted(() => ({
  listSpaceMembers: vi.fn(),
  addSpaceMember: vi.fn(),
  updateSpaceMember: vi.fn(),
  removeSpaceMember: vi.fn(),
  addSpaceGroupMember: vi.fn(),
  updateSpaceGroupMember: vi.fn(),
  removeSpaceGroupMember: vi.fn(),
  searchMemberCandidates: vi.fn(),
  searchGroupCandidates: vi.fn(),
  listSpaces: vi.fn(),
}))
vi.mock('@/api/generated', () => api)

const member = (over: object) => ({ id: 'm', space_id: 's1', user_id: null, group_id: null, role: 'editor', created_at: '', user_email: null, user_display_name: null, group_name: null, ...over })
const rows = [
  member({ id: 'm1', user_id: 'u1', role: 'owner', user_display_name: 'Olive', user_email: 'o@x.test' }),
  member({ id: 'm2', user_id: 'u2', role: 'viewer', user_display_name: 'Vic', user_email: 'v@x.test' }),
  member({ id: 'm3', group_id: 'g1', role: 'editor', group_name: 'staff' }),
]

function mountMembers (my_role: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSpacesStore().spaces = [{ id: 's1', name: 'Eng', my_role }] as never
  return mount(SpaceMembers, {
    props: { spaceId: 's1' },
    global: { plugins: [pinia, createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] },
  })
}

describe('space members screen', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  beforeEach(() => {
    // Vuetify overlays (the confirm dialog) need these browser APIs; jsdom lacks them.
    vi.stubGlobal('ResizeObserver', class {
      observe () {} unobserve () {} disconnect () {}
    })
    vi.stubGlobal('visualViewport', { width: 1024, height: 768, offsetLeft: 0, offsetTop: 0, scale: 1, addEventListener () {}, removeEventListener () {} })
    for (const fn of Object.values(api)) {
      fn.mockReset()
    }
    api.listSpaceMembers.mockResolvedValue({ data: rows })
  })

  it('is hidden (and requests nothing) below admin', async () => {
    const w = mountMembers('editor')
    await flushPromises()
    expect(api.listSpaceMembers).not.toHaveBeenCalled()
    expect(w.find('[data-testid="user-members"]').exists()).toBe(false)
    expect(w.text()).toContain('Only space owners and admins')
  })

  it('lists users and groups and lets an owner remove a member', async () => {
    api.removeSpaceMember.mockResolvedValue({})
    const w = mountMembers('owner')
    await flushPromises()
    expect(w.find('[data-testid="user-members"]').text()).toContain('Vic')
    expect(w.find('[data-testid="group-members"]').text()).toContain('staff')
    await w.find('[data-testid="remove-u2"]').trigger('click')
    await flushPromises()
    // The confirm dialog teleports to document.body.
    const confirm = document.body.querySelector('[data-testid="confirm-action"]') as HTMLElement
    confirm.click()
    await flushPromises()
    expect(api.removeSpaceMember).toHaveBeenCalledWith({ path: { spaceId: 's1', userId: 'u2' } })
  })

  it('does not let an admin touch the owner row', async () => {
    const w = mountMembers('admin')
    await flushPromises()
    expect(w.find('[data-testid="remove-u1"]').exists()).toBe(false)
    expect(w.find('[data-testid="remove-u2"]').exists()).toBe(true)
  })

  it('shows the last-owner 409 message verbatim', async () => {
    api.removeSpaceMember.mockResolvedValue({ error: { error: 'cannot remove the last owner of a space' } })
    const w = mountMembers('owner')
    await flushPromises()
    await w.find('[data-testid="remove-u1"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector('[data-testid="confirm-action"]') as HTMLElement).click()
    await flushPromises()
    expect(w.find('[data-testid="members-error"]').text()).toContain('cannot remove the last owner of a space')
  })
})
