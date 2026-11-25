import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { setGuestToken } from '@/api/client'
import { redeemShareLink, resolveShareLink } from '@/api/generated'
import { attachmentBlobCache } from '@/utils/attachmentBlobCache'
import { isTokenExpired } from '@/utils/jwt'
import { retryAfterSeconds } from '@/utils/rateLimit'

// Share-link guest session (Decisions 6, 21). Kept entirely separate from the
// member auth store: a guest token is never written to the member session
// slot, and while a guest session is active it is the one the api client and
// collab use (api/client.ts setGuestToken). Persisted in sessionStorage (this
// tab only, gone when it closes) keyed by link token so a reload doesn't
// re-prompt and mint a second guest identity.
const KEY_PREFIX = 'little-doc-editor.guest.'

export interface GuestSession {
  linkToken: string
  token: string
  permission: 'view' | 'edit'
  pageId: string
  displayName: string
}

export interface ResolvedLink {
  requiresPassword: boolean
  permission: 'view' | 'edit'
  pageTitle?: string
}

// Failure kinds the share page maps to messages.
export type ShareFailure = 'notFound' | 'gone' | 'rateLimited' | 'wrongPassword' | 'invalid' | 'network'

export class ShareError extends Error {
  readonly kind: ShareFailure
  // Seconds from Retry-After on a 429, when the api sent one.
  readonly retryAfter: number | null
  constructor (kind: ShareFailure, retryAfter: number | null = null) {
    super(kind)
    this.name = 'ShareError'
    this.kind = kind
    this.retryAfter = retryAfter
  }
}

function kindForStatus (status: number | undefined): ShareFailure {
  switch (status) {
    case 400: { return 'invalid' }
    case 401: { return 'wrongPassword' }
    case 404: { return 'notFound' }
    case 410: { return 'gone' }
    case 429: { return 'rateLimited' }
    default: { return 'network' }
  }
}

export const useGuestStore = defineStore('guest', () => {
  const session = ref<GuestSession | null>(null)
  const expired = ref(false)

  const isGuest = computed(() => session.value !== null)

  function activate (next: GuestSession) {
    session.value = next
    expired.value = false
    setGuestToken(next.token, () => {
      expired.value = true
      clear(next.linkToken)
    })
  }

  // Drops the session (and the stored copy) so the next visit re-prompts.
  function clear (linkToken?: string) {
    const token = linkToken ?? session.value?.linkToken
    if (token) {
      sessionStorage.removeItem(KEY_PREFIX + token)
    }
    deactivate()
  }

  // Leaves the share route without forgetting the redeemed session.
  function deactivate () {
    session.value = null
    setGuestToken(null)
    // Bytes fetched under the guest token must not outlive the guest session.
    attachmentBlobCache.clear()
  }

  function restore (linkToken: string): boolean {
    const raw = sessionStorage.getItem(KEY_PREFIX + linkToken)
    if (!raw) {
      return false
    }
    try {
      const stored = JSON.parse(raw) as GuestSession
      if (stored.linkToken !== linkToken || typeof stored.token !== 'string' || isTokenExpired(stored.token)) {
        sessionStorage.removeItem(KEY_PREFIX + linkToken)
        return false
      }
      activate(stored)
      return true
    } catch {
      sessionStorage.removeItem(KEY_PREFIX + linkToken)
      return false
    }
  }

  async function resolve (linkToken: string): Promise<ResolvedLink> {
    const { data, response } = await resolveShareLink({ path: { token: linkToken } }).catch(() => ({ data: undefined, response: undefined }))
    if (!data) {
      throw new ShareError(kindForStatus(response?.status), retryAfterSeconds(response))
    }
    return data
  }

  // The display name is mandatory before any access (Decision 6); the api
  // refuses an empty one too. The member's bearer token (if signed in) goes
  // along automatically so the access log can name them — api/client.ts only
  // swaps in the guest token once this session is active.
  async function redeem (linkToken: string, displayName: string, password?: string): Promise<GuestSession> {
    const name = displayName.trim()
    if (!name) {
      throw new ShareError('invalid')
    }
    const { data, response } = await redeemShareLink({
      path: { token: linkToken },
      body: { displayName: name, ...(password ? { password } : {}) },
    }).catch(() => ({ data: undefined, response: undefined }))
    if (!data) {
      throw new ShareError(kindForStatus(response?.status), retryAfterSeconds(response))
    }
    const next: GuestSession = { linkToken, token: data.token, permission: data.permission, pageId: data.pageId, displayName: name }
    sessionStorage.setItem(KEY_PREFIX + linkToken, JSON.stringify(next))
    activate(next)
    return next
  }

  return { session, expired, isGuest, resolve, redeem, restore, clear, deactivate }
})
