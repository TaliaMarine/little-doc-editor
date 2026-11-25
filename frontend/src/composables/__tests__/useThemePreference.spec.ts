import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { createVuetify } from 'vuetify'
import { useThemePreference } from '@/composables/useThemePreference'
import { THEME_STORAGE_KEY } from '@/utils/themePreference'

function setup () {
  let api!: ReturnType<typeof useThemePreference>
  const Comp = defineComponent({
    setup () {
      api = useThemePreference()
      return () => h('div')
    },
  })
  mount(Comp, { global: { plugins: [createVuetify({ theme: { defaultTheme: 'system' } })] } })
  return api
}

describe('useThemePreference', () => {
  beforeEach(() => localStorage.clear())

  it('reads the system preference by default', () => {
    expect(setup().preference.value).toBe('system')
  })

  it('changes and persists the choice', async () => {
    const api = setup()
    api.setPreference('dark')
    await Promise.resolve()
    expect(api.preference.value).toBe('dark')
    expect(api.isDark.value).toBe(true)
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
    api.setPreference('system')
    await Promise.resolve()
    expect(api.preference.value).toBe('system')
  })
})
