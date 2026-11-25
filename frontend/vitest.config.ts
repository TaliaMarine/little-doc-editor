import { fileURLToPath, URL } from 'node:url'
import Vue from '@vitejs/plugin-vue'
import Vuetify from 'vite-plugin-vuetify'
import { defineConfig } from 'vitest/config'

// Standalone rather than merged with vite.config.mts: tests don't need the
// fonts/PWA plugins. Vue + Vuetify auto-import are needed for component tests.
export default defineConfig({
  plugins: [Vue(), Vuetify({ autoImport: true })],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('src', import.meta.url)),
    },
  },
  test: {
    include: ['src/**/__tests__/*.spec.ts'],
    environment: 'jsdom',
    server: { deps: { inline: ['vuetify'] } },
    css: false,
  },
})
