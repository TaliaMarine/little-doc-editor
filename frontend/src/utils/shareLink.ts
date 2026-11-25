import type { ShareLink } from '@/api/generated'

// The public link for a token. Only the token ever goes in the URL (it is the
// capability); never the page id.
export function shareLinkUrl (origin: string, token: string): string {
  return `${origin}/share/${encodeURIComponent(token)}`
}

export type ShareLinkStatus = 'active' | 'expired' | 'revoked'

export function shareLinkStatus (link: Pick<ShareLink, 'revoked_at' | 'expires_at'>, now = Date.now()): ShareLinkStatus {
  if (link.revoked_at) {
    return 'revoked'
  }
  if (link.expires_at && new Date(link.expires_at).getTime() <= now) {
    return 'expired'
  }
  return 'active'
}

// `<input type="datetime-local">` value (local time, no zone) -> RFC 3339.
// null when empty, unparsable or not in the future (the api rejects those).
export function expiryToIso (local: string, now = Date.now()): string | null {
  if (!local) {
    return null
  }
  const time = new Date(local).getTime()
  return Number.isNaN(time) || time <= now ? null : new Date(time).toISOString()
}

// Which SPA path a link inside shared content should navigate to: only pages
// of the shared tree are reachable, so an in-app page link becomes a link
// within the share route (the api refuses anything outside the link's scope).
export function sharePathForHref (href: string, origin: string, linkToken: string): string | null {
  let url: URL
  try {
    url = new URL(href, origin)
  } catch {
    return null
  }
  if (url.origin !== origin) {
    return null
  }
  const match = /^\/(?:spaces\/[^/]+\/pages|share\/[^/]+)\/([0-9a-f-]{36})\/?$/i.exec(url.pathname)
  return match ? `/share/${encodeURIComponent(linkToken)}/${match[1]!.toLowerCase()}` : null
}
