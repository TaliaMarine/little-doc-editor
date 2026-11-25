// WebSocket Origin allow-list (COLLAB_ALLOWED_ORIGINS, comma-separated).
//
// Browsers always send Origin on a WebSocket handshake and let any page open
// a socket to any host, so without this a hostile page could drive a victim's
// collab session if it ever got hold of a token. The token is the real
// credential (it is sent in-band, never as a cookie), so this is defence in
// depth: unset/empty = no check (the previous behaviour); set = a handshake
// that carries an Origin outside the list is refused with 403. A handshake
// with no Origin header is not a browser (curl, server-side client) and is
// left to token auth.

// Throws on an entry that could never match a browser's Origin (wildcard, path,
// non-http(s)), so a typo fails startup instead of 403ing every connection.
export function parseAllowedOrigins (raw: string | undefined): string[] {
  return (raw ?? '').split(',').map(e => e.trim()).filter(e => e !== '').map((entry) => {
    let u: URL
    try { u = new URL(entry) } catch { throw new Error(`COLLAB_ALLOWED_ORIGINS entry "${entry}" is not an origin like https://docs.example.com`) }
    if ((u.protocol !== 'http:' && u.protocol !== 'https:') || (u.pathname !== '/' && u.pathname !== '') || u.search || u.hash || u.username) {
      throw new Error(`COLLAB_ALLOWED_ORIGINS entry "${entry}" is not an origin like https://docs.example.com`)
    }
    return u.origin.toLowerCase() // drops default ports, like browsers do
  })
}

// Lowercased, no trailing slash, so `https://Docs.example.com/` matches the
// browser's `https://docs.example.com`.
export function normalizeOrigin (o: string): string {
  return o.trim().replace(/\/+$/, '').toLowerCase()
}

// allowed: parsed list. An empty list permits everything.
export function isOriginAllowed (origin: string | undefined, allowed: readonly string[]): boolean {
  if (allowed.length === 0 || origin === undefined) return true
  return allowed.includes(normalizeOrigin(origin))
}
