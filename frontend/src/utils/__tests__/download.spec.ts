import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ExportError, fetchExport, filenameFromDisposition, printHtmlBlob } from '@/utils/download'

const exportPage = vi.fn()
const exportSpace = vi.fn()
vi.mock('@/api/generated', () => ({
  exportPage: (...a: unknown[]) => exportPage(...a),
  exportSpace: (...a: unknown[]) => exportSpace(...a),
}))

function ok (blob: Blob, headers: Record<string, string> = {}) {
  return { data: blob, response: new Response(null, { status: 200, headers }) }
}

describe('filenameFromDisposition', () => {
  it('reads quoted, bare and RFC 5987 names and strips path separators', () => {
    expect(filenameFromDisposition('attachment; filename="a.zip"')).toBe('a.zip')
    expect(filenameFromDisposition('attachment; filename=b.md')).toBe('b.md')
    expect(filenameFromDisposition('attachment; filename*=UTF-8\'\'caf%C3%A9.html')).toBe('café.html')
    expect(filenameFromDisposition('attachment; filename="../../x.md"')).toBe('.._.._x.md')
    expect(filenameFromDisposition(null)).toBeNull()
    expect(filenameFromDisposition('inline')).toBeNull()
  })
})

describe('fetchExport', () => {
  beforeEach(() => {
    exportPage.mockReset()
    exportSpace.mockReset()
  })

  it('requests a blob for the page, with descendants when asked', async () => {
    exportPage.mockResolvedValue(ok(new Blob(['zip'], { type: 'application/zip' }), { 'Content-Disposition': 'attachment; filename="tree.zip"' }))
    const result = await fetchExport({ kind: 'page', id: 'p1', descendants: true }, 'md')
    expect(exportPage).toHaveBeenCalledWith({ path: { pageId: 'p1' }, query: { format: 'md', descendants: true }, parseAs: 'blob' })
    expect(result.filename).toBe('tree.zip')
  })

  it('falls back to a typed filename without Content-Disposition', async () => {
    exportSpace.mockResolvedValue(ok(new Blob(['z'], { type: 'application/zip' })))
    expect((await fetchExport({ kind: 'space', id: 's1' }, 'html')).filename).toBe('space-export.zip')
    exportPage.mockResolvedValue(ok(new Blob(['# x'], { type: 'text/markdown' })))
    expect((await fetchExport({ kind: 'page', id: 'p' }, 'md')).filename).toBe('page-export.md')
  })

  it('maps 413 and server errors to ExportError', async () => {
    exportSpace.mockResolvedValue({ error: {}, response: new Response(null, { status: 413 }) })
    await expect(fetchExport({ kind: 'space', id: 's' }, 'md')).rejects.toMatchObject({ status: 413 })
    exportSpace.mockResolvedValue({ error: { error: 'nope' }, response: new Response(null, { status: 403 }) })
    await expect(fetchExport({ kind: 'space', id: 's' }, 'md')).rejects.toBeInstanceOf(ExportError)
    await expect(fetchExport({ kind: 'space', id: 's' }, 'md')).rejects.toThrow('nope')
  })
})

describe('printHtmlBlob', () => {
  it('loads the html into a hidden sandboxed iframe and prints it', async () => {
    const print = vi.fn()
    const original = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, 'contentWindow')!
    Object.defineProperty(HTMLIFrameElement.prototype, 'contentWindow', {
      configurable: true,
      get () {
        return { print, focus: vi.fn(), addEventListener: vi.fn() }
      },
    })
    const promise = printHtmlBlob(new Blob(['<p>hi</p>'], { type: 'text/html' }))
    // jsdom doesn't load srcdoc; fire the load event ourselves once appended.
    await vi.waitFor(() => expect(document.querySelector('iframe')).not.toBeNull())
    const frame = document.querySelector('iframe')!
    expect(frame.getAttribute('sandbox')).not.toContain('allow-scripts')
    expect(frame.srcdoc).toBe('<p>hi</p>')
    frame.dispatchEvent(new Event('load'))
    await promise
    expect(print).toHaveBeenCalledOnce()
    frame.remove()
    Object.defineProperty(HTMLIFrameElement.prototype, 'contentWindow', original)
  })
})
