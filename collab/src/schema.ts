// The ProseMirror schema collab uses to convert between page content
// (pages.content, ProseMirror/Tiptap JSON) and Yjs documents.
//
// !!! KEEP IN SYNC WITH THE FRONTEND !!!
// This list must produce the same schema as the editor's shared extensions
// module (frontend/src/editor/extensions.ts): every node/mark name and every
// attribute name must match. The y-tiptap binding maps a Yjs XmlFragment to
// ProseMirror nodes by node *name* and attribute *name*; a mismatch makes the
// client (or this server) fail to read the other side's content — or silently
// drop it. Only schema-affecting parts matter here (names, attrs, content
// expressions, marks); rendering, NodeViews, commands and keymaps do not.
//
// Contract with the frontend:
//   - StarterKit with the undo/redo ("history") extension DISABLED — Yjs
//     provides undo (Tiptap 3: `undoRedo: false`). Everything else StarterKit
//     ships (incl. link, underline) stays on so the schema matches.
//   - `attachmentImage`: inline atom node, attrs `attachmentId` and `alt`
//     (Image-by-attachment-ID; never a URL).
//   - Mermaid diagrams are ordinary `codeBlock` nodes with attr
//     language = 'mermaid' — no separate node type, so nothing to add here.
//   - Yjs fragment name is Tiptap's default, 'default' (YDOC_FIELD).
// When the frontend adds a node/mark/attribute, add the schema part here too
// and extend the round-trip test in collab/test/convert.test.ts.
import { getSchema, Node } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'

export const AttachmentImage = Node.create({
  name: 'attachmentImage',
  group: 'inline',
  inline: true,
  atom: true,
  draggable: true,
  addAttributes () {
    return {
      attachmentId: { default: null },
      alt: { default: null },
    }
  },
  parseHTML () {
    return [{ tag: 'img[data-attachment-id]' }]
  },
  renderHTML ({ node }) {
    return ['img', { 'data-attachment-id': node.attrs.attachmentId, alt: node.attrs.alt }]
  },
})

export const extensions = [
  StarterKit.configure({ undoRedo: false }),
  AttachmentImage,
]

export const schema = getSchema(extensions)

// Tiptap's Collaboration extension default Y.XmlFragment name.
export const YDOC_FIELD = 'default'
