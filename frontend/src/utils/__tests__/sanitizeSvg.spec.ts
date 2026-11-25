import { describe, expect, it } from 'vitest'
import { sanitizeSvg, sanitizeSvgBlob } from '@/utils/sanitizeSvg'

const evil = '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(1)</script><a href="javascript:alert(1)"><rect width="5" height="5" onclick="x()"/></a></svg>'

describe('sanitizeSvg', () => {
  it('strips scripts, handlers and javascript: urls but keeps shapes', () => {
    const out = sanitizeSvg(evil)
    expect(out).not.toMatch(/script|onload|onclick|javascript:/i)
    expect(out).toContain('<rect')
  })

  it('keeps mermaid-style foreignObject labels', () => {
    const out = sanitizeSvg('<svg xmlns="http://www.w3.org/2000/svg"><foreignObject width="10" height="10"><div xmlns="http://www.w3.org/1999/xhtml"><span class="nodeLabel">Hi</span></div></foreignObject></svg>')
    expect(out).toContain('Hi')
    expect(out.toLowerCase()).toContain('foreignobject')
  })

  it('sanitizes svg blobs and passes other types through', async () => {
    const clean = await sanitizeSvgBlob(new Blob([evil], { type: 'image/svg+xml' }))
    expect(clean.type).toBe('image/svg+xml')
    expect(await clean.text()).not.toMatch(/script|onload/i)
    const png = new Blob(['x'], { type: 'image/png' })
    expect(await sanitizeSvgBlob(png)).toBe(png)
  })
})

describe('sanitizeSvgBlob media types', () => {
  it('sanitizes SVGs served with a charset or odd casing', async () => {
    const out = await sanitizeSvgBlob(new Blob([evil], { type: 'Image/SVG+xml; charset=utf-8' }))
    expect(await out.text()).not.toMatch(/script|onload/i)
  })
})
