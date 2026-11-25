// Runtime configuration (served as /config.js by the container, see
// docker/40-lde-config.sh). Precedence: window.__LDE_CONFIG__, then VITE_*
// build-time env, then the same-origin defaults.
export interface LdeConfig {
  apiBaseUrl?: string
  collabUrl?: string
}

declare global {
  interface Window {
    __LDE_CONFIG__?: LdeConfig
  }
}

export function runtimeConfig (): LdeConfig {
  return (typeof window === 'undefined' ? undefined : window.__LDE_CONFIG__) ?? {}
}

export function resolveApiBaseUrl (runtime: LdeConfig, envUrl: string | undefined): string {
  return runtime.apiBaseUrl || envUrl || '/api/v1'
}

export function resolveCollabSetting (runtime: LdeConfig, envUrl: string | undefined): string | undefined {
  return runtime.collabUrl || envUrl || undefined
}
