import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import SearchDialog from '@/components/SearchDialog.vue'
import i18nPlugin from '@/plugins/i18n'

vi.stubGlobal('visualViewport', { width: 1024, height: 768, offsetLeft: 0, offsetTop: 0, scale: 1, addEventListener: vi.fn(), removeEventListener: vi.fn() })
const searchSpace = vi.fn()
vi.mock('@/api/generated', () => ({ searchSpace: (...a: unknown[]) => searchSpace(...a) }))
const push = vi.fn()
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))

function mountDialog () {
  return mount(SearchDialog, {
    attachTo: document.body,
    props: { modelValue: true, spaceId: 's1', spaceName: 'Docs' },
    global: { plugins: [createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] },
  })
}

// v-dialog teleports to body.
const q = (sel: string) => document.body.querySelector<HTMLElement>(sel)
async function type (value: string) {
  const input = q('[data-testid="search-input"] input') as HTMLInputElement
  input.value = value
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}
async function settle () {
  vi.advanceTimersByTime(300)
  await flushPromises()
}

describe('SearchDialog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    searchSpace.mockReset()
    push.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('debounces, shows titles as text, and navigates on Enter', async () => {
    searchSpace.mockResolvedValue({ data: [{ id: 'p1', title: '<img src=x onerror=alert(1)>' }, { id: 'p2', title: 'Second' }], response: { status: 200 } })
    const w = mountDialog()
    await flushPromises()
    await type('h')
    await type('he')
    await type('hello')
    expect(searchSpace).not.toHaveBeenCalled()
    await settle()
    expect(searchSpace).toHaveBeenCalledTimes(1)
    expect(searchSpace).toHaveBeenCalledWith({ path: { spaceId: 's1' }, query: { q: 'hello' } })
    expect(q('[data-testid="search-hit-0"]')?.textContent).toContain('<img src=x onerror=alert(1)>')
    expect(document.body.querySelector('[data-testid="search-results"] img')).toBeNull()

    const input = q('[data-testid="search-input"] input')!
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await flushPromises()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(push).toHaveBeenCalledWith('/spaces/s1/pages/p2')
    w.unmount()
  })

  it('does not search blank queries and shows the empty state', async () => {
    searchSpace.mockResolvedValue({ data: [], response: { status: 200 } })
    const w = mountDialog()
    await flushPromises()
    await type(' '.repeat(3))
    await settle()
    expect(searchSpace).not.toHaveBeenCalled()
    await type('zzz')
    await settle()
    expect(q('[data-testid="search-empty"]')).not.toBeNull()
    w.unmount()
  })

  it('reports a 429 with the retry delay', async () => {
    searchSpace.mockResolvedValue({ error: { error: 'slow down' }, response: { status: 429, headers: new Headers({ 'Retry-After': '12' }) } })
    const w = mountDialog()
    await flushPromises()
    await type('x')
    await settle()
    expect(document.body.textContent).toContain('12 seconds')
    w.unmount()
  })
})
