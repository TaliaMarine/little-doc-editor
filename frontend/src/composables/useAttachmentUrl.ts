import { type MaybeRefOrGetter, onBeforeUnmount, ref, type Ref, toValue, watch } from 'vue'
import { attachmentBlobCache } from '@/utils/attachmentBlobCache'

// Resolves an attachment ID to a blob URL for the lifetime of the calling
// component, releasing it on unmount or when the ID changes.
export function useAttachmentUrl (id: MaybeRefOrGetter<string | null | undefined>): { url: Ref<string | null>, error: Ref<boolean>, loading: Ref<boolean> } {
  const url = ref<string | null>(null)
  const error = ref(false)
  const loading = ref(false)
  let held: string | null = null

  function drop () {
    if (held) {
      attachmentBlobCache.release(held)
    }
    held = null
  }

  watch(() => toValue(id), async next => {
    drop()
    url.value = null
    error.value = false
    if (!next) {
      return
    }
    held = next
    loading.value = true
    try {
      const resolved = await attachmentBlobCache.acquire(next)
      if (held === next) {
        url.value = resolved || null
        // '' = the cache was cleared (e.g. logout) mid-fetch.
        error.value = !resolved
      }
    } catch {
      if (held === next) {
        // The failed entry isn't cached, so there's nothing to release.
        held = null
        error.value = true
      }
    } finally {
      if (held === next) {
        loading.value = false
      }
    }
  }, { immediate: true })

  onBeforeUnmount(drop)
  return { url, error, loading }
}
