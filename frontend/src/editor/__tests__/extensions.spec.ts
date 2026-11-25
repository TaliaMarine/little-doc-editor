import { Editor } from '@tiptap/core'
import { prosemirrorJSONToYDoc, yDocToProsemirrorJSON } from '@tiptap/y-tiptap'
import { describe, expect, it } from 'vitest'
import { Awareness } from 'y-protocols/awareness'
import * as Y from 'yjs'
import { createExtensions } from '@/editor/extensions'

const doc = {
  type: 'doc',
  content: [
    { type: 'paragraph', content: [{ type: 'text', text: 'hi ' }, { type: 'attachmentImage', attrs: { attachmentId: 'abc-123', alt: 'pic.png' } }] },
    { type: 'codeBlock', attrs: { language: 'mermaid' }, content: [{ type: 'text', text: 'graph TD\n A-->B' }] },
  ],
}

describe('editor extensions', () => {
  it('stores attachmentImage as an ID, never a URL, in JSON and HTML', () => {
    const editor = new Editor({ extensions: createExtensions(), content: doc })
    expect(editor.getJSON().content![0]!.content![1]).toEqual({ type: 'attachmentImage', attrs: { attachmentId: 'abc-123', alt: 'pic.png' } })
    const html = editor.getHTML()
    expect(html).toContain('data-attachment-id="abc-123"')
    expect(html).not.toContain('src=')
    editor.destroy()
  })

  it('round-trips attachmentImage through HTML', () => {
    const editor = new Editor({ extensions: createExtensions(), content: doc })
    const again = new Editor({ extensions: createExtensions(), content: editor.getHTML() })
    expect((again.getJSON().content![0]!.content![1] as { attrs: unknown }).attrs).toEqual({ attachmentId: 'abc-123', alt: 'pic.png' })
    editor.destroy()
    again.destroy()
  })

  it('survives a Yjs round trip with the schema collab uses (mermaid stays a codeBlock)', () => {
    const editor = new Editor({ extensions: createExtensions(), content: doc })
    const ydoc = prosemirrorJSONToYDoc(editor.schema, doc, 'default')
    const copy = new Y.Doc()
    Y.applyUpdate(copy, Y.encodeStateAsUpdate(ydoc))
    const json = yDocToProsemirrorJSON(copy, 'default') as { content: any[] }
    expect(json.content![1]).toMatchObject({ type: 'codeBlock', attrs: { language: 'mermaid' } })
    expect(json.content![0]!.content![1]).toMatchObject({ type: 'attachmentImage', attrs: { attachmentId: 'abc-123' } })
    editor.destroy()
  })

  it('disables the built-in history only for collab', () => {
    const names = (editor: Editor) => editor.extensionManager.extensions.map(e => e.name)
    const solo = new Editor({ extensions: createExtensions() })
    expect(names(solo)).toContain('undoRedo')
    expect(names(solo)).not.toContain('collaboration')

    const ydoc = new Y.Doc()
    const provider = { awareness: new Awareness(ydoc) } as never
    const live = new Editor({ extensions: createExtensions({ collab: { document: ydoc, provider, user: { name: 'A', color: '#fff' } } }) })
    expect(names(live)).not.toContain('undoRedo')
    expect(names(live)).toContain('collaboration')
    expect(names(live)).toContain('collaborationCaret')
    solo.destroy()
    live.destroy()
  })
})
