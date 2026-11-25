// Attachment transport helpers (SPEC.md Decisions 17, 32, 34).
//
// Download goes through the generated client (it carries the bearer token).
// Upload is the one hand-written request: the fetch-based generated client
// can't report upload progress, and progress is part of the §1 UI. It reuses
// the client's base URL + token, and still routes a 401 to the shared
// unauthorized handler, so it stays equivalent to `uploadPageAttachment`.
import type { Attachment } from '@/api/generated'
import { currentSession, getBaseUrl, notifyUnauthorized } from '@/api/client'
import { downloadAttachment } from '@/api/generated'
import i18n from '@/plugins/i18n'
import { sanitizeSvgBlob } from '@/utils/sanitizeSvg'

const t = i18n.global.t

export class UploadError extends Error {
  readonly status: number
  constructor (message: string, status: number) {
    super(message)
    this.name = 'UploadError'
    this.status = status
  }
}

export interface UploadOptions {
  onProgress?: (fraction: number) => void
  signal?: AbortSignal
}

// Pulls the server's `{ error }` message out of a response body. 422s carry
// the policy message that must be shown as-is.
export function errorMessageFromBody (body: string, fallback: string): string {
  try {
    const parsed = JSON.parse(body) as { error?: unknown }
    if (typeof parsed.error === 'string' && parsed.error) {
      return parsed.error
    }
  } catch {
    // not JSON — fall through
  }
  return fallback
}

export function uploadAttachment (pageId: string, file: File, options: UploadOptions = {}): Promise<Attachment> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', `${getBaseUrl()}/pages/${encodeURIComponent(pageId)}/attachments`)
    const sent = currentSession()
    const token = sent.token
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    xhr.upload.addEventListener('progress', event => {
      if (event.lengthComputable && event.total > 0) {
        options.onProgress?.(event.loaded / event.total)
      }
    })
    xhr.addEventListener('load', () => {
      if (xhr.status === 401) {
        notifyUnauthorized(sent)
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText) as Attachment)
        } catch {
          reject(new UploadError(t('attachments.uploadFailed', { status: xhr.status }), xhr.status))
        }
        return
      }
      reject(new UploadError(errorMessageFromBody(xhr.responseText, t('attachments.uploadFailed', { status: xhr.status })), xhr.status))
    })
    xhr.addEventListener('error', () => reject(new UploadError(t('attachments.networkError'), 0)))
    xhr.addEventListener('abort', () => reject(new UploadError(t('attachments.uploadCancelled'), 0)))
    options.signal?.addEventListener('abort', () => xhr.abort(), { once: true })

    const body = new FormData()
    body.append('file', file, file.name)
    xhr.send(body)
  })
}

export async function fetchAttachmentBlob (attachmentId: string): Promise<Blob> {
  const { data, error } = await downloadAttachment({ path: { attachmentId }, parseAs: 'blob' })
  if (error || !data) {
    throw new Error((error as { error?: string } | undefined)?.error ?? t('attachments.loadFailed'))
  }
  return sanitizeSvgBlob(data as unknown as Blob)
}
