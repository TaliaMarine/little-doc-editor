import { describe, expect, it } from 'vitest'
import { loadThemePreference, saveThemePreference, THEME_STORAGE_KEY } from '@/utils/themePreference'

function fakeStorage (initial?: string) {
  const data = new Map<string, string>(initial === undefined ? [] : [[THEME_STORAGE_KEY, initial]])
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
  }
}

describe('themePreference', () => {
  it('defaults to system when unset or invalid', () => {
    expect(loadThemePreference(fakeStorage())).toBe('system')
    expect(loadThemePreference(fakeStorage('purple'))).toBe('system')
  })
  it('round-trips a saved choice', () => {
    const storage = fakeStorage()
    saveThemePreference('dark', storage)
    expect(loadThemePreference(storage)).toBe('dark')
  })
  it('survives throwing storage', () => {
    const blocked = () => {
      throw new Error('blocked')
    }
    const broken = { getItem: blocked, setItem: blocked }
    expect(loadThemePreference(broken)).toBe('system')
    expect(() => saveThemePreference('light', broken)).not.toThrow()
  })
})
