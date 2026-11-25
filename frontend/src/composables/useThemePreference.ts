import { computed } from 'vue'
import { useTheme } from 'vuetify'
import { isThemePreference, saveThemePreference, type ThemePreference } from '@/utils/themePreference'

// The initial theme is applied in plugins/vuetify.ts (defaultTheme); this
// only changes + persists it.
export function useThemePreference () {
  const theme = useTheme()
  const preference = computed<ThemePreference>(() => {
    // `global.name` is the resolved theme ('light'/'dark') even when following
    // the OS, so ask `isSystem` first.
    if (theme.isSystem.value) {
      return 'system'
    }
    const name = theme.global.name.value
    return isThemePreference(name) ? name : 'system'
  })
  const isDark = computed(() => theme.current.value.dark)

  function setPreference (value: ThemePreference) {
    theme.change(value)
    saveThemePreference(value)
  }

  return { preference, isDark, setPreference }
}
