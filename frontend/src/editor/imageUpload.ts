import { Extension } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'

export interface ImageUploadOptions {
  // Uploads an image and resolves with its attachment ID. Reject to abort the
  // insert (the caller is responsible for surfacing the message).
  upload: ((file: File) => Promise<string>) | null
  // Non-image files dropped/pasted into the editor are handed over here (they
  // become page attachments, not inline content).
  uploadFile?: ((file: File) => Promise<unknown>) | null
}

export function isImageFile (file: File): boolean {
  return file.type.startsWith('image/')
}

// Paste/drop of files into the editor. Images are uploaded and inserted as
// `attachmentImage` nodes (ID only); other files are attached to the page.
// Without an `upload` callback (read-only viewers) the extension is inert.
export const ImageUpload = Extension.create<ImageUploadOptions>({
  name: 'imageUpload',

  addOptions () {
    return { upload: null, uploadFile: null }
  },

  addProseMirrorPlugins () {
    // eslint-disable-next-line unicorn/no-this-outside-of-class -- Tiptap extension context
    const { editor, options } = this

    // Insert at the dropped position, clamped to the document as it is when
    // the upload finishes (remote/local edits may have moved things), and
    // without stealing focus.
    function insertImage (attachmentId: string, alt: string, position: number | undefined) {
      if (editor.isDestroyed) {
        return
      }
      const node = { type: 'attachmentImage', attrs: { attachmentId, alt } }
      if (position === undefined) {
        editor.chain().insertContent(node).run()
      } else {
        editor.chain().insertContentAt(Math.min(position, editor.state.doc.content.size), node).run()
      }
    }

    function handleFiles (files: File[], position?: number): boolean {
      if (files.length === 0) {
        return false
      }
      // Claim the event even when we can't use the files, so the browser
      // doesn't navigate away to the dropped file (read-only viewers).
      if (!options.upload || !editor.isEditable) {
        return true
      }
      for (const file of files) {
        if (isImageFile(file)) {
          options.upload(file)
            .then(attachmentId => insertImage(attachmentId, file.name, position))
            .catch(() => {})
        } else {
          options.uploadFile?.(file)?.catch(() => {})
        }
      }
      return true
    }

    return [
      new Plugin({
        key: new PluginKey('imageUpload'),
        props: {
          handlePaste (_view, event) {
            const data = event.clipboardData
            // Rich copies (Word, browsers) carry both text/html and a rendered
            // image: prefer the text and only take file-only pastes.
            if (!data || data.types.includes('text/html') || data.types.includes('text/plain')) {
              return false
            }
            return handleFiles([...data.files])
          },
          handleDrop (view, event) {
            const files = [...(event.dataTransfer?.files ?? [])]
            if (files.length === 0) {
              return false
            }
            const coords = view.posAtCoords({ left: event.clientX, top: event.clientY })
            return handleFiles(files, coords?.pos)
          },
        },
      }),
    ]
  },
})
