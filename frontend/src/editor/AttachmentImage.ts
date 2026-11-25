import { mergeAttributes, Node } from '@tiptap/core'
import { VueNodeViewRenderer } from '@tiptap/vue-3'
import AttachmentImageView from './AttachmentImageView.vue'

// Inline image that references an uploaded attachment by ID — never a URL
// (SPEC.md Decision 34, §6.3). Saved content and Yjs state therefore hold only
// `attachmentId`; the NodeView resolves it to an authenticated blob: URL at
// render time. Both attrs are plain strings, so Yjs syncs them as ordinary
// XML-element attributes.
export const AttachmentImage = Node.create({
  name: 'attachmentImage',
  group: 'inline',
  inline: true,
  atom: true,
  draggable: true,
  selectable: true,

  addAttributes () {
    return {
      attachmentId: {
        default: null,
        parseHTML: element => element.dataset.attachmentId ?? null,
        renderHTML: attributes => attributes.attachmentId ? { 'data-attachment-id': attributes.attachmentId } : {},
      },
      alt: {
        // null (not '') to match collab/src/schema.ts exactly.
        default: null,
        parseHTML: element => element.getAttribute('alt'),
      },
    }
  },

  parseHTML () {
    return [{ tag: 'img[data-attachment-id]' }]
  },

  // Deliberately no `src`: the ID is the only thing serialized.
  renderHTML ({ HTMLAttributes }) {
    return ['img', mergeAttributes(HTMLAttributes)]
  },

  renderText ({ node }) {
    return node.attrs.alt ?? ''
  },

  addNodeView () {
    return VueNodeViewRenderer(AttachmentImageView)
  },
})
