import { computed, reactive, ref, shallowRef } from 'vue'
import { resolveCollabSetting, runtimeConfig } from '@/config'
import { type CollabConnection, type CollabSession, colorForName, createCollabSession, type PresenceUser, resolveCollabUrl } from '@/editor/collabSession'
import { useAuthStore } from '@/stores/auth'

// How long the initial connect+sync may take before the editor falls back to
// a read-only view of the last persisted content.
const INITIAL_SYNC_TIMEOUT_MS = 8000

// `guest` is a share-link guest session (the token redeemed from the link,
// never the member's); otherwise the signed-in member's session is used.
export function useCollabSession (pageId: string, guest?: { token: string, displayName: string }) {
  const authStore = useAuthStore()
  const connection = ref<CollabConnection>('connecting')
  const failureReason = ref('')
  const everSynced = ref(false)
  const unsynced = ref(0)
  const serverReadOnly = ref(false)
  const timedOut = ref(false)
  const users = ref<PresenceUser[]>([])

  // The display name always comes from a real identity: the member's account
  // or the name the guest entered in the mandatory prompt (Decision 6).
  const username = guest?.displayName ?? authStore.displayName ?? ''
  const user = reactive({ name: username, color: colorForName(username) })

  const session = shallowRef<CollabSession>(createCollabSession({
    url: resolveCollabUrl(resolveCollabSetting(runtimeConfig(), import.meta.env.VITE_COLLAB_URL), window.location, import.meta.env.DEV),
    pageId,
    getToken: () => guest?.token ?? authStore.token ?? '',
  }, {
    onConnection (state, detail) {
      // A refused session (bad token / no access) is permanent: the socket
      // closing right after must not downgrade it to a generic "offline".
      if (connection.value === 'failed' && state === 'disconnected') {
        return
      }
      connection.value = state
      failureReason.value = state === 'failed' ? (detail ?? '') : ''
    },
    onSynced (synced) {
      if (synced) {
        everSynced.value = true
        timedOut.value = false
      }
    },
    onUnsynced (count) {
      unsynced.value = count
    },
    onReadOnly (readOnly) {
      serverReadOnly.value = readOnly
    },
    onUsers (list) {
      users.value = list
    },
  }))

  let timer = setTimeout(() => {
    if (!everSynced.value) {
      timedOut.value = true
    }
  }, INITIAL_SYNC_TIMEOUT_MS)

  // Never got a usable document: show the persisted content read-only instead
  // of an editor that would silently keep edits nobody else will see.
  const unavailable = computed(() => !everSynced.value && (connection.value === 'failed' || timedOut.value))

  // One entry per connection (awareness carries only a display name, so two
  // people who share a name must not be merged); the local client first.
  const people = computed(() => users.value.toSorted((a, b) => Number(b.self) - Number(a.self)))

  function retry () {
    timedOut.value = false
    connection.value = 'connecting'
    clearTimeout(timer)
    timer = setTimeout(() => {
      if (!everSynced.value) {
        timedOut.value = true
      }
    }, INITIAL_SYNC_TIMEOUT_MS)
    session.value.retry()
  }

  // Warn before leaving the tab while edits haven't reached the server.
  function beforeUnload (event: BeforeUnloadEvent) {
    if (unsynced.value > 0) {
      event.preventDefault()
    }
  }
  window.addEventListener('beforeunload', beforeUnload)

  function destroy () {
    clearTimeout(timer)
    window.removeEventListener('beforeunload', beforeUnload)
    session.value.destroy()
  }
  return {
    session, user, connection, failureReason, everSynced, unsynced, serverReadOnly,
    unavailable, people, retry, destroy,
  }
}
