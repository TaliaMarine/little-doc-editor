// Message catalogs live per locale in src/locales/<code>/ (SPEC.md Decision 13
// — i18n is in v1 scope). Add new keys there rather than hardcoding
// user-facing text in components. Locales are discovered by glob, so a new
// locale is just a new folder with an index.ts default export.
import type en from '@/locales/en'
import { createI18n } from 'vue-i18n'

const modules = import.meta.glob<{ default: typeof en }>('../locales/*/index.ts', { eager: true })

const messages: Record<string, typeof en> = {}
for (const [path, module] of Object.entries(modules)) {
  const code = path.split('/').at(-2)!
  messages[code] = module.default
}

export default createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages,
})
