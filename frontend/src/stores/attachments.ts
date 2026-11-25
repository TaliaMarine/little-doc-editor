import type { Attachment } from '@/api/generated'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { uploadAttachment, UploadError } from '@/api/attachments'
import { listPageAttachments } from '@/api/generated'
import i18n from '@/plugins/i18n'

const t = i18n.global.t

export interface UploadItem {
  id: number
  filename: string
  progress: number
  status: 'uploading' | 'error'
  // The server's message, shown as-is (422 policy rejections).
  error?: string
}

// Holds the attachments of the page currently open plus in-flight/failed
// uploads. Shared by the panel, the editor's paste/drop handler and the
// share-target flow so they all go through one upload path.
export const useAttachmentsStore = defineStore('attachments', () => {
  const pageId = ref<string | null>(null)
  const items = ref<Attachment[]>([])
  const uploads = ref<UploadItem[]>([])
  const loading = ref(false)
  const loadError = ref('')
  let nextUploadId = 1

  async function fetchFor (id: string) {
    if (pageId.value !== id) {
      pageId.value = id
      items.value = []
      uploads.value = []
    }
    loading.value = true
    loadError.value = ''
    try {
      const { data, error } = await listPageAttachments({ path: { pageId: id } })
      if (error) {
        throw new Error(error.error ?? t('attachments.listFailed'))
      }
      // Ignore a response for a page we've already navigated away from.
      if (pageId.value === id) {
        items.value = data ?? []
      }
    } catch (error) {
      if (pageId.value === id) {
        loadError.value = error instanceof Error ? error.message : t('attachments.listFailed')
      }
    } finally {
      if (pageId.value === id) {
        loading.value = false
      }
    }
  }

  // Uploads one file to `targetPageId`. Resolves with the attachment, or
  // rejects with the server's message (kept in `uploads` for display until
  // dismissed).
  async function upload (targetPageId: string, file: File): Promise<Attachment> {
    const entry: UploadItem = { id: nextUploadId++, filename: file.name, progress: 0, status: 'uploading' }
    const tracked = pageId.value === targetPageId
    if (tracked) {
      uploads.value.push(entry)
    }
    // Reactive proxy of the pushed entry, so progress updates re-render.
    const live = tracked ? uploads.value.find(u => u.id === entry.id)! : entry
    try {
      const attachment = await uploadAttachment(targetPageId, file, {
        onProgress: fraction => {
          live.progress = fraction
        },
      })
      uploads.value = uploads.value.filter(u => u.id !== entry.id)
      if (pageId.value === targetPageId) {
        items.value = [attachment, ...items.value]
      }
      return attachment
    } catch (error) {
      live.status = 'error'
      live.error = error instanceof UploadError || error instanceof Error ? error.message : String(error)
      throw error
    }
  }

  function dismissUpload (id: number) {
    uploads.value = uploads.value.filter(u => u.id !== id)
  }

  function reset () {
    pageId.value = null
    items.value = []
    uploads.value = []
    loadError.value = ''
  }

  return { pageId, items, uploads, loading, loadError, fetchFor, upload, dismissUpload, reset }
})

export { type Attachment } from '@/api/generated'
