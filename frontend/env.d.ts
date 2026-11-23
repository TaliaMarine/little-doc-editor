/// <reference types="vite/client" />
/// <reference types="vite-plugin-vue-layouts-next/client" />

interface ImportMetaEnv {
  // Base URL for the api service, e.g. "https://docs.example.com/api/v1".
  // Defaults to the relative "/api/v1" (same-origin behind one ingress, or
  // the dev server's proxy in vite.config.mts) when unset.
  readonly VITE_API_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
