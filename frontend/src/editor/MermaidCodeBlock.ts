import CodeBlock from '@tiptap/extension-code-block'
import { VueNodeViewRenderer } from '@tiptap/vue-3'
import CodeBlockView from './CodeBlockView.vue'

// Mermaid is NOT a separate node: it's the stock `codeBlock` node with
// language 'mermaid' (SPEC.md §6.10 — a fenced ```mermaid block). That keeps
// the document schema, Yjs state and server-side conversion (collab) identical
// for diagrams and plain code; only this NodeView differs, rendering a live
// preview for the mermaid language. The diagram source stays ordinary
// ProseMirror text inside the node, so concurrent edits merge in Yjs.
export const MermaidCodeBlock = CodeBlock.extend({
  addNodeView () {
    return VueNodeViewRenderer(CodeBlockView)
  },
})
