// Authenticated attachment bytes -> `blob:` URLs (SPEC.md Decision 34).
//
// `GET /attachments/{id}` needs the bearer token, so <img src>/<a href> can't
// load it directly. Consumers `acquire(id)` a blob URL and `release(id)` when
// they unmount. Entries with no holders stay cached (LRU, capped) so
// revisiting a page doesn't refetch; evicted/cleared entries are revoked.
import { fetchAttachmentBlob } from '@/api/attachments'

interface Entry {
  url: Promise<string>
  resolved: string | null
  refs: number
  type: string | null
}

export interface BlobCacheDeps {
  fetchBlob: (id: string) => Promise<Blob>
  createObjectURL: (blob: Blob) => string
  revokeObjectURL: (url: string) => void
  maxIdle: number
}

export function createAttachmentBlobCache (deps: BlobCacheDeps) {
  // Map iteration order = insertion order; we re-insert on release so the
  // oldest idle entry is first.
  const entries = new Map<string, Entry>()

  function destroy (id: string) {
    const entry = entries.get(id)
    entries.delete(id)
    if (entry?.resolved) {
      deps.revokeObjectURL(entry.resolved)
    }
  }

  function trimIdle () {
    let idle = 0
    for (const entry of entries.values()) {
      if (entry.refs === 0) {
        idle++
      }
    }
    for (const [id, entry] of entries) {
      if (idle <= deps.maxIdle) {
        break
      }
      if (entry.refs === 0) {
        destroy(id)
        idle--
      }
    }
  }

  function acquire (id: string): Promise<string> {
    let entry = entries.get(id)
    if (!entry) {
      const created: Entry = { url: Promise.resolve(''), resolved: null, refs: 0, type: null }
      created.url = deps.fetchBlob(id).then(blob => {
        created.type = blob.type
        // The entry may have been cleared while the fetch was in flight.
        if (entries.get(id) !== created) {
          return ''
        }
        created.resolved = deps.createObjectURL(blob)
        return created.resolved
      })
      // A failed fetch must not be cached — the next acquire retries.
      created.url.catch(() => {
        if (entries.get(id) === created) {
          entries.delete(id)
        }
      })
      entries.set(id, created)
      entry = created
    }
    entry.refs++
    return entry.url
  }

  function release (id: string) {
    const entry = entries.get(id)
    if (!entry) {
      return
    }
    entry.refs = Math.max(0, entry.refs - 1)
    if (entry.refs === 0) {
      // Move to the idle end of the LRU order.
      entries.delete(id)
      entries.set(id, entry)
      trimIdle()
    }
  }

  function clear () {
    for (const id of entries.keys()) {
      destroy(id)
    }
  }

  return { acquire, release, clear, size: () => entries.size }
}

export const attachmentBlobCache = createAttachmentBlobCache({
  fetchBlob: fetchAttachmentBlob,
  createObjectURL: blob => URL.createObjectURL(blob),
  revokeObjectURL: url => URL.revokeObjectURL(url),
  maxIdle: 50,
})
