// Live collaboration session for one page: a Y.Doc + Hocuspocus provider
// against the collab service (SPEC.md §4). Framework-light so it's testable;
// the Vue-facing state lives in `useCollabSession`.
import { HocuspocusProvider, WebSocketStatus } from '@hocuspocus/provider'
import * as Y from 'yjs'

export type CollabConnection = 'connecting' | 'connected' | 'disconnected' | 'failed'

// collab refuses any document name that isn't the canonical lowercase UUID.
export function collabDocumentName (pageId: string): string {
  return pageId.toLowerCase()
}

// Dev default matches `just collab` (:1234). In production set VITE_COLLAB_URL
// to the public ws(s):// endpoint, or leave it unset to use same-origin
// `/collab` (ingress routes it to the collab service).
export function resolveCollabUrl (envUrl: string | undefined, location: Pick<Location, 'protocol' | 'host'>, dev: boolean): string {
  if (envUrl?.startsWith('/')) {
    // Path-only setting (e.g. "/collab"): same host, ws(s) scheme.
    return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${envUrl}`
  }
  if (envUrl) {
    return envUrl
  }
  if (dev) {
    return 'ws://localhost:1234'
  }
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/collab`
}

// Deterministic per-name caret colour.
const COLORS = ['#e53935', '#8e24aa', '#3949ab', '#039be5', '#00897b', '#7cb342', '#fb8c00', '#6d4c41']
export function colorForName (name: string): string {
  let hash = 0
  for (const char of name) {
    hash = (hash * 31 + char.codePointAt(0)!) % 2_147_483_647
  }
  return COLORS[Math.abs(hash) % COLORS.length]!
}

export interface CollabHandlers {
  onConnection: (state: CollabConnection, detail?: string) => void
  onSynced: (synced: boolean) => void
  onUnsynced: (count: number) => void
  onReadOnly: (readOnly: boolean) => void
  onUsers: (users: PresenceUser[]) => void
}

export interface PresenceUser {
  clientId: number
  name: string
  color: string
  self: boolean
}

export interface CollabSession {
  document: Y.Doc
  provider: HocuspocusProvider
  destroy: () => void
  retry: () => void
}

export function createCollabSession (opts: {
  url: string
  pageId: string
  getToken: () => string
}, handlers: CollabHandlers): CollabSession {
  const document = new Y.Doc()
  const provider: HocuspocusProvider = new HocuspocusProvider({
    url: opts.url,
    name: collabDocumentName(opts.pageId),
    document,
    token: opts.getToken,
    onStatus: ({ status }) => {
      if (status === WebSocketStatus.Connected) {
        handlers.onConnection('connected')
      } else if (status === WebSocketStatus.Connecting) {
        handlers.onConnection('connecting')
      } else {
        handlers.onConnection('disconnected')
      }
    },
    onAuthenticated: ({ scope }) => handlers.onReadOnly(scope === 'readonly'),
    // Permanent: bad/expired token or no access to this page. The provider
    // won't recover by retrying, so surface it distinctly.
    onAuthenticationFailed: ({ reason }) => handlers.onConnection('failed', reason),
    onSynced: ({ state }) => handlers.onSynced(state),
    onUnsyncedChanges: ({ number }) => handlers.onUnsynced(number),
    onAwarenessChange: ({ states }) => {
      const self = document.clientID
      handlers.onUsers(states
        .filter(state => state.user?.name)
        .map(state => ({
          clientId: state.clientId,
          name: String(state.user.name),
          color: String(state.user.color ?? '#888'),
          self: state.clientId === self,
        })))
    },
  })

  return {
    document,
    provider,
    destroy () {
      provider.destroy()
      document.destroy()
    },
    retry () {
      provider.connect().catch(() => {})
    },
  }
}
