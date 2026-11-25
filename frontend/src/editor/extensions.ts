import type { HocuspocusProvider } from '@hocuspocus/provider'
// The single editor extension list (NEXT.md §4/§5). Everything that builds a
// Tiptap editor — live collab, the read-only fallback, tests — goes through
// `createExtensions`, so the ProseMirror schema never diverges between paths
// (a divergent schema silently breaks Yjs sync and server-side conversion).
//
// Node names that collab (server-side Yjs <-> JSON conversion) relies on:
// the StarterKit nodes, `attachmentImage`, and mermaid as a `codeBlock` with
// language 'mermaid' (no separate node). Yjs fragment name: 'default'.
import type { AnyExtension } from '@tiptap/core'
import type * as Y from 'yjs'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCaret from '@tiptap/extension-collaboration-caret'
import StarterKit from '@tiptap/starter-kit'
import { AttachmentImage } from './AttachmentImage'
import { ImageUpload, type ImageUploadOptions } from './imageUpload'
import { MermaidCodeBlock } from './MermaidCodeBlock'

export interface ExtensionOptions {
  // Present => live collaboration: StarterKit's own history is disabled (Yjs
  // provides undo/redo) and Collaboration/CollaborationCaret are added.
  collab?: { document: Y.Doc, provider: HocuspocusProvider, user: { name: string, color: string } }
  imageUpload?: Partial<ImageUploadOptions>
}

export function createExtensions (options: ExtensionOptions = {}): AnyExtension[] {
  const extensions: AnyExtension[] = [
    StarterKit.configure({
      codeBlock: false,
      // Its appendTransaction would write a trailing paragraph into the Yjs
      // doc from every client (viewers included) and duplicate across editors.
      trailingNode: false,
      // Link clicks must not window.open() without noopener (guest tab
      // hijack); in-app links are handled by the share route instead.
      link: { openOnClick: false },
      // Tiptap v3 name for the history extension. Must be off with Collaboration.
      ...(options.collab ? { undoRedo: false as const } : {}),
    }),
    MermaidCodeBlock,
    AttachmentImage,
    ImageUpload.configure({ upload: null, uploadFile: null, ...options.imageUpload }),
  ]
  if (options.collab) {
    extensions.push(
      Collaboration.configure({ document: options.collab.document }),
      CollaborationCaret.configure({ provider: options.collab.provider, user: options.collab.user }),
    )
  }
  return extensions
}
