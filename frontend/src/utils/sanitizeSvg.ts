// Defence in depth for SVG that ends up in the DOM or opened from a blob: URL
// (mermaid output, SVG attachments), behind the SPA's CSP. Scripts, event
// handlers and javascript: URLs are stripped; mermaid's foreignObject labels
// are kept (same allowances mermaid itself uses with DOMPurify).
import DOMPurify from 'dompurify'

const OPTIONS = {
  USE_PROFILES: { svg: true, svgFilters: true, html: true },
  ADD_TAGS: ['foreignobject'],
  ADD_ATTR: ['dominant-baseline'],
  HTML_INTEGRATION_POINTS: { foreignobject: true },
}

export function sanitizeSvg (svg: string): string {
  return DOMPurify.sanitize(svg, OPTIONS)
}

// Re-wraps an SVG blob after sanitizing it; other types pass through. Parsed
// and serialized as XML so the result stays a valid standalone image/svg+xml.
export async function sanitizeSvgBlob (blob: Blob): Promise<Blob> {
  const mediaType = blob.type.split(';', 1)[0]!.trim().toLowerCase()
  if (mediaType !== 'image/svg+xml') {
    return blob
  }
  const clean = DOMPurify.sanitize(await blob.text(), { ...OPTIONS, PARSER_MEDIA_TYPE: 'image/svg+xml' })
  return new Blob([clean], { type: 'image/svg+xml' })
}
