import { describe, expect, it } from 'vitest'
import * as Y from 'yjs'
import { contentToYDoc, docToContent, extractPlainText, normalizeContent, sameContent } from '../src/convert.js'
import { doc } from './fixtures.js'

const rich = {
  type: 'doc',
  content: [
    { type: 'heading', attrs: { level: 2 }, content: [{ type: 'text', text: 'Title' }] },
    { type: 'paragraph', content: [
      { type: 'text', text: 'hel', marks: [{ type: 'bold' }] },
      { type: 'text', text: 'lo ' },
      { type: 'attachmentImage', attrs: { attachmentId: 'a1b2', alt: 'pic' } },
    ] },
    { type: 'codeBlock', attrs: { language: 'mermaid' }, content: [{ type: 'text', text: 'graph TD; A-->B' }] },
    { type: 'bulletList', content: [{ type: 'listItem', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'x' }] }] }] },
  ],
}

describe('convert', () => {
  it('round-trips rich content including attachmentImage and mermaid code blocks', () => {
    const back = docToContent(contentToYDoc(rich))
    expect(back.content![1].content![2]).toEqual({ type: 'attachmentImage', attrs: { attachmentId: 'a1b2', alt: 'pic' } })
    expect(back.content![2].attrs).toMatchObject({ language: 'mermaid' })
    expect(extractPlainText(back)).toBe(extractPlainText(rich))
  })

  it('is idempotent after normalisation', () => {
    const once = normalizeContent(rich)
    expect(sameContent(once, normalizeContent(once))).toBe(true)
  })

  it('does not crash on unknown nodes/marks; keeps their children', () => {
    const weird = {
      type: 'doc',
      content: [
        { type: 'futureCallout', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'kept', marks: [{ type: 'sparkle' }] }] }] },
        { type: 'mysteryLeaf' },
      ],
    }
    expect(extractPlainText(docToContent(contentToYDoc(weird)))).toBe('kept')
  })

  it('repairs structurally invalid content instead of throwing', () => {
    const bad = { type: 'doc', content: [{ type: 'text', text: 'loose' }] }
    expect(() => contentToYDoc(bad)).not.toThrow()
  })

  it('treats {} and empty docs as blank', () => {
    expect(contentToYDoc({}).getXmlFragment('default').length).toBe(0)
    expect(contentToYDoc({ type: 'doc' }).getXmlFragment('default').length).toBe(0)
  })

  it('seeds deterministically so concurrent seeds merge without duplication', () => {
    const a = Y.encodeStateAsUpdate(contentToYDoc(doc('one', 'two')))
    const b = Y.encodeStateAsUpdate(contentToYDoc(doc('one', 'two')))
    expect(Buffer.from(a).equals(Buffer.from(b))).toBe(true)
    const merged = new Y.Doc()
    Y.applyUpdate(merged, a)
    Y.applyUpdate(merged, b)
    expect(extractPlainText(docToContent(merged))).toBe('one two')
  })
})

describe('extractPlainText (parity with api searchindex.ExtractPlainText)', () => {
  it('joins every text node with a single space, across marks', () => {
    expect(extractPlainText({
      type: 'doc',
      content: [{ type: 'paragraph', content: [
        { type: 'text', text: 'hel', marks: [{ type: 'bold' }] }, { type: 'text', text: 'lo' },
      ] }, { type: 'paragraph', content: [{ type: 'text', text: 'world' }] }],
    })).toBe('hel lo world')
  })
  it('returns empty string for empty content', () => {
    expect(extractPlainText({})).toBe('')
  })
})
