---
name: add-tiptap-node
description: This skill should be used when the user asks to "add a Tiptap node", "add a custom editor extension", "add a new block type", "add a mermaid-style block", or wants a new ProseMirror node for the little-doc-editor editor. Covers schema, Vue NodeView, Markdown round-trip, and Yjs-collab safety for custom Tiptap nodes.
---

# Adding a custom Tiptap node

little-doc-editor's editor is Tiptap (ProseMirror-based) with Markdown import/export extensions and the official Yjs collaboration extension (SPEC.md §5). The existing Mermaid node (a fenced ` ```mermaid ` block rendered live client-side, §6.10) is the reference implementation to imitate — read its extension file before starting a new one.

## Design questions to answer first

1. **Is this inline content or a block?** Blocks (like Mermaid) usually want `group: 'block'`, `atom: true` if the content isn't directly editable as ProseMirror text (e.g. a diagram source string edited via a custom UI rather than inline typing).
2. **What attributes does it need**, and are any of them large blobs (e.g. diagram source, code content) that should be stored as a single attribute string rather than as nested ProseMirror text nodes?
3. **Does it need a Markdown source representation?** If it should round-trip through export/import (Decision 4: Markdown is generated on demand, never the stored format, but export/import still needs a faithful mapping), it needs a fenced-code-block-like Markdown syntax, matching the Mermaid node's approach unless there's a reason to diverge.

## Steps

1. **Define the ProseMirror node schema** as a Tiptap `Node` extension: `name`, `group`, `content` (if any), `atom`/`selectable`/`draggable` as appropriate, and `addAttributes()` for its data.
2. **Implement `parseHTML`/`renderHTML`** (or `renderText`) so the node round-trips through Tiptap's own serialization, and `addNodeView()` returning a Vue component if it needs custom rendering (e.g. a live-rendered diagram, an image with a caption UI) — model the Vue NodeView on the Mermaid node's component, including how it handles read-only vs. editable state.
3. **Wire Markdown import/export** by extending (or matching the pattern of) the Markdown extension already used for import/export — the node needs a way to serialize to and parse from its fenced/inline Markdown form. Test round-tripping: create the node in the editor → export to Markdown → import that Markdown back → confirm the node and its attributes are identical, not just visually similar.
4. **Register the extension in the shared editor config** used by both the solo-editing path and the Yjs-collaborative path — check where the existing extension list lives (likely a single shared Tiptap `extensions` array/config module) and add it there, not in a per-view-only setup. A node registered only on one path will desync collaborators.
5. **Verify Yjs sync doesn't corrupt the node's attributes.** Large attribute blobs (e.g. diagram source) sync fine as Yjs XML-fragment attributes, but confirm by testing two simulated clients editing the node's content concurrently (or sequentially, if concurrent editing of that attribute isn't meant to be supported) and checking the final state matches on both sides.
6. **Add a way to insert it**: a toolbar button and/or slash-command entry, consistent with how existing block types are inserted.
7. **Add to page templates if relevant** — if this node type should be available in the built-in templates (Decision 22), check whether any existing template content needs updating.

## Common mistakes to avoid

- Registering the extension in only one of the solo/collab editor configs (breaks parity between the two paths).
- Storing large content (diagram source, code) as nested text nodes instead of a single attribute, making Markdown round-trip and Yjs sync fragile.
- Skipping the Markdown round-trip test — a node that renders correctly in the live editor but doesn't survive export→import will silently corrupt content the first time someone exports and re-imports a page.
- Forgetting `atom: true`/`selectable` handling for nodes whose content isn't meant to be directly editable as ProseMirror text, which can let a user accidentally place a cursor inside internal state and corrupt it.
