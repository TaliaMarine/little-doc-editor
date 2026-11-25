// Post-login redirect targets come from a query string / sessionStorage, so
// only plain same-app paths are accepted: no scheme-relative (`//`, `/\`)
// targets, no fragment (could smuggle `/auth/callback#token=…`), and never
// the OIDC callback itself.
export function safeRedirect (value: unknown, fallback = '/'): string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) {
    return fallback
  }
  const path = value.split('#', 1)[0]!
  if (path === '/auth/callback' || path.startsWith('/auth/callback?') || path.startsWith('/auth/callback/')) {
    return fallback
  }
  return path || fallback
}
