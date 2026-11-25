import { describe, expect, it } from 'vitest'
import { collabDocumentName, colorForName, resolveCollabUrl } from '@/editor/collabSession'

describe('collabSession helpers', () => {
  it('uses the canonical lowercase page id as the document name', () => {
    expect(collabDocumentName('3F2504E0-4F89-11D3-9A0C-0305E82C3301')).toBe('3f2504e0-4f89-11d3-9a0c-0305e82c3301')
  })

  it('resolves the collab URL', () => {
    const loc = { protocol: 'https:', host: 'docs.example.com' }
    expect(resolveCollabUrl('wss://x/y', loc, false)).toBe('wss://x/y')
    expect(resolveCollabUrl(undefined, loc, true)).toBe('ws://localhost:1234')
    expect(resolveCollabUrl(undefined, loc, false)).toBe('wss://docs.example.com/collab')
    expect(resolveCollabUrl(undefined, { protocol: 'http:', host: 'h' }, false)).toBe('ws://h/collab')
  })

  it('picks a stable colour per name', () => {
    expect(colorForName('Ada')).toBe(colorForName('Ada'))
    expect(colorForName('Ada')).toMatch(/^#/)
  })
})
