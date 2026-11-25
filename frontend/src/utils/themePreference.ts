// Theme choice persisted in localStorage. 'system' follows the OS
// (prefers-color-scheme) via Vuetify's built-in `system` theme.
export type ThemePreference = 'system' | 'light' | 'dark'

export const THEME_STORAGE_KEY = 'lde.theme'
export const THEME_PREFERENCES: readonly ThemePreference[] = ['system', 'light', 'dark']

export function isThemePreference (value: unknown): value is ThemePreference {
  return typeof value === 'string' && (THEME_PREFERENCES as readonly string[]).includes(value)
}

export function loadThemePreference (storage: Pick<Storage, 'getItem'> | undefined = globalThis.localStorage): ThemePreference {
  try {
    const raw = storage?.getItem(THEME_STORAGE_KEY)
    return isThemePreference(raw) ? raw : 'system'
  } catch {
    return 'system'
  }
}

export function saveThemePreference (value: ThemePreference, storage: Pick<Storage, 'setItem'> | undefined = globalThis.localStorage): void {
  try {
    storage?.setItem(THEME_STORAGE_KEY, value)
  } catch {
    // Storage blocked (private mode): the choice just doesn't persist.
  }
}
