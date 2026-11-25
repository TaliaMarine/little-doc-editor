import { type ExportFormat, exportPage, exportSpace } from '@/api/generated'

export interface ExportResult {
  blob: Blob
  filename: string
}

export type ExportTarget
  = | { kind: 'page', id: string, descendants?: boolean }
    | { kind: 'space', id: string }

// Parses `filename="x"` / `filename*=UTF-8''x` from a Content-Disposition
// header; null when absent. Never trusts path separators.
export function filenameFromDisposition (header: string | null): string | null {
  if (!header) {
    return null
  }
  const star = /filename\*\s*=\s*[\w-]*'[^']*'([^;]+)/i.exec(header)
  let name: string | null = null
  if (star) {
    try {
      name = decodeURIComponent(star[1]!.trim())
    } catch {
      name = null
    }
  }
  if (!name) {
    const plain = /filename\s*=\s*(?:"([^"]*)"|([^;]+))/i.exec(header)
    name = (plain?.[1] ?? plain?.[2] ?? '').trim() || null
  }
  return name ? name.replaceAll(/[\\/]/g, '_') : null
}

function fallbackName (target: ExportTarget, format: ExportFormat, blobType: string): string {
  const zip = blobType.includes('zip')
  const ext = zip ? 'zip' : (format === 'md' ? 'md' : 'html')
  return `${target.kind}-export.${ext}`
}

// Authenticated fetch through the generated client (bearer interceptor +
// 401 handling), asking for a Blob so zips survive intact (Decision 34).
export async function fetchExport (target: ExportTarget, format: ExportFormat): Promise<ExportResult> {
  const result = target.kind === 'page'
    ? await exportPage({
        path: { pageId: target.id },
        query: { format, descendants: target.descendants ? true : undefined },
        parseAs: 'blob',
      })
    : await exportSpace({ path: { spaceId: target.id }, query: { format }, parseAs: 'blob' })
  const { data, error, response } = result as { data?: unknown, error?: unknown, response: Response }
  if (error !== undefined || !response?.ok || !(data instanceof Blob)) {
    if (response?.status === 413) {
      throw new ExportError('too_many', 413)
    }
    const message = typeof error === 'object' && error && 'error' in error ? String((error as { error: unknown }).error) : ''
    throw new ExportError(message || 'failed', response?.status ?? 0)
  }
  const filename = filenameFromDisposition(response.headers.get('Content-Disposition')) ?? fallbackName(target, format, data.type)
  return { blob: data, filename }
}

export class ExportError extends Error {
  status: number
  constructor (message: string, status: number) {
    super(message)
    this.status = status
  }
}

export function saveBlob (blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

// PDF export: the api returns print-optimised HTML; the browser prints it.
// The html carries its own CSP meta (Decision 44), and the iframe is sandboxed
// without scripts so even a CSP bypass can't run code in our origin;
// `allow-modals` + `allow-same-origin` are what print() needs from the parent.
export async function printHtmlBlob (blob: Blob): Promise<void> {
  const html = await blob.text()
  const iframe = document.createElement('iframe')
  iframe.setAttribute('aria-hidden', 'true')
  iframe.setAttribute('sandbox', 'allow-same-origin allow-modals')
  iframe.style.cssText = 'position:fixed;right:0;bottom:0;width:0;height:0;border:0;visibility:hidden'
  const cleanup = () => iframe.remove()
  await new Promise<void>((resolve, reject) => {
    iframe.addEventListener('load', () => {
      try {
        const win = iframe.contentWindow
        if (!win) {
          throw new Error('print frame unavailable')
        }
        win.addEventListener('afterprint', cleanup)
        win.focus()
        win.print()
        // Some browsers never fire afterprint; don't leak the frame forever.
        setTimeout(cleanup, 120_000)
        resolve()
      } catch (error) {
        cleanup()
        reject(error as Error)
      }
    }, { once: true })
    iframe.srcdoc = html
    document.body.append(iframe)
  })
}
