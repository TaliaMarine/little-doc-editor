import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'

// Deliberately standalone rather than merged with vite.config.mts: the
// current tests are plain TypeScript (no component mounting), so they don't
// need the Vue/Vuetify/fonts plugins — just the `@` alias. Merge in the vite
// config (and add jsdom + @vue/test-utils) when the first component test
// lands.
export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('src', import.meta.url)),
    },
  },
  test: {
    include: ['src/**/__tests__/*.spec.ts'],
  },
})
