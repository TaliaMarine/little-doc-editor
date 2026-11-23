// Message catalog kept in one place per locale so strings stay extractable
// (SPEC.md Decision 13 — i18n is in v1 scope). Add new keys here rather than
// hardcoding user-facing text in components.
import { createI18n } from 'vue-i18n'

const messages = {
  en: {
    app: {
      title: 'little-doc-editor',
    },
    nav: {
      spaces: 'Spaces',
      noSpaces: 'No spaces yet',
      pages: 'Pages',
      noPages: 'No pages yet',
      loadFailed: 'Failed to load pages',
      logout: 'Log out',
    },
    profile: {
      admin: 'Administrator',
      editor: 'Editor',
      sso: 'Single sign-on',
      internal: 'Local account',
      username: 'Username',
      groups: 'Groups',
      lastLogin: 'Last sign-in',
    },
    home: {
      welcome: 'Welcome to little-doc-editor',
      subtitle: 'Select a space from the sidebar, or create one to get started.',
    },
    login: {
      title: 'Log in',
      email: 'Email',
      password: 'Password',
      submit: 'Log in',
      error: 'Login failed',
      sso: 'Sign in with SSO',
      ssoFailed: 'Single sign-on failed',
    },
    space: {
      selectOrCreate: 'Select a page from the sidebar, or create one to get started.',
      select: 'Select a page from the sidebar.',
      newPage: 'New page',
      untitled: 'Untitled',
      createFailed: 'Failed to create page',
    },
    editor: {
      titleLabel: 'Title',
      save: 'Save',
      saved: 'Saved',
      saveFailed: 'Failed to save',
      loadFailed: 'Failed to load page',
      locked: 'This page is locked and cannot be edited.',
      readOnly: 'You have read-only access to this page.',
    },
  },
}

export default createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages,
})
