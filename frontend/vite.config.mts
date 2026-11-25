import { fileURLToPath, URL } from 'node:url'
import Vue from '@vitejs/plugin-vue'
import Fonts from 'unplugin-fonts/vite'
import { defineConfig } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'
import Vuetify, { transformAssetUrls } from 'vite-plugin-vuetify'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    Vue({
      template: { transformAssetUrls },
    }),
    // https://github.com/vuetifyjs/vuetify-loader/tree/master/packages/vite-plugin#readme
    Vuetify({
      autoImport: true,
      styles: {
        configFile: 'src/styles/settings.scss',
      },
    }),
    // Installability + Android share target only (SPEC.md §5.1, Decision 31):
    // injectManifest with a hand-written worker (sw/sw.ts) because generateSW
    // can't intercept the share POST. No precache (injectionPoint unset) and
    // no offline page-content caching.
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'sw',
      filename: 'sw.ts',
      registerType: 'autoUpdate',
      injectRegister: false,
      injectManifest: { injectionPoint: undefined },
      manifest: {
        name: 'little-doc-editor',
        short_name: 'little-doc',
        description: 'Collaborative document editor',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#ffffff',
        theme_color: '#1867c0',
        icons: [
          { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
        ],
        // Exact shape from SPEC.md §5.1 (one file per share action, Decision 32).
        // Not in the plugin's manifest typings yet.

        share_target: {
          action: '/share-target',
          method: 'POST',
          enctype: 'multipart/form-data',
          params: {
            title: 'title',
            text: 'text',
            files: [{ name: 'file', accept: ['*/*'] }],
          },
        },
      },
    }),
    Fonts({
      fontsource: {
        families: [
          {
            name: 'Roboto',
            weights: [100, 300, 400, 500, 700, 900],
            styles: ['normal', 'italic'],
          },
        ],
      },
    }),
  ],
  define: { 'process.env': {} },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('src', import.meta.url)),
    },
    extensions: [
      '.js',
      '.json',
      '.jsx',
      '.mjs',
      '.ts',
      '.tsx',
      '.vue',
    ],
  },
  server: {
    port: 3000,
    // Routes browser calls to the api service through this same-origin dev
    // server, avoiding CORS entirely (the api service has no CORS
    // middleware — see DEVELOPER.md). Production is expected to serve
    // frontend/api same-origin behind one ingress; VITE_API_URL below is
    // the escape hatch if that's ever not the case.
    proxy: {
      // LDE_API_PROXY lets a second stack (own port/DB) run beside `just api`.
      '/api': process.env.LDE_API_PROXY ?? 'http://localhost:8080',
    },
  },
})
