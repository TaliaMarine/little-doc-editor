import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import PageActions from '@/components/PageActions.vue'
import i18nPlugin from '@/plugins/i18n'

const setPageLocked = vi.fn()
const deletePage = vi.fn()
vi.mock('@/api/generated', () => ({
  setPageLocked: (...a: unknown[]) => setPageLocked(...a),
  deletePage: (...a: unknown[]) => deletePage(...a),
  listPagesBySpace: vi.fn().mockResolvedValue({ data: [] }),
  listPageTemplates: vi.fn().mockResolvedValue({ data: [] }),
  getPage: vi.fn(),
  createPage: vi.fn(),
  updatePageContent: vi.fn(),
  exportPage: vi.fn(),
  exportSpace: vi.fn(),
}))
const push = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

function mountActions (my_role: 'viewer' | 'editor' | 'owner' | null, locked = false) {
  return mount(PageActions, {
    props: { page: { id: 'p1', space_id: 's1', title: 'Doc', locked, my_role } },
    global: { plugins: [createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] },
  })
}

describe('PageActions gating', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setPageLocked.mockReset()
    deletePage.mockReset()
    push.mockReset()
  })

  it('hides delete/lock/sub-page for viewers but keeps export', () => {
    const w = mountActions('viewer')
    expect(w.find('[data-testid="delete-page"]').exists()).toBe(false)
    expect(w.find('[data-testid="toggle-lock"]').exists()).toBe(false)
    expect(w.find('[data-testid="new-subpage"]').exists()).toBe(false)
    expect(w.find('[data-testid="export-menu"]').exists()).toBe(true)
  })

  it('hides everything for share-link guests (no role)', () => {
    const w = mountActions(null)
    expect(w.find('[data-testid="delete-page"]').exists()).toBe(false)
    expect(w.find('[data-testid="export-menu"]').exists()).toBe(false)
  })

  it('shows management actions to editors and toggles the lock', async () => {
    setPageLocked.mockResolvedValue({ data: { id: 'p1', space_id: 's1', locked: true } })
    const w = mountActions('editor')
    expect(w.find('[data-testid="delete-page"]').exists()).toBe(true)
    await w.find('[data-testid="toggle-lock"]').trigger('click')
    await flushPromises()
    expect(setPageLocked).toHaveBeenCalledWith({ path: { pageId: 'p1' }, body: { locked: true } })
  })

  it('shows Unlock on a locked page', () => {
    const w = mountActions('editor', true)
    expect(w.find('[data-testid="toggle-lock"]').text()).toBe('Unlock page')
  })
})
