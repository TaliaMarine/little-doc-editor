// Tiny builders for ProseMirror/Tiptap JSON (SPEC.md Decision 4), limited to
// the nodes the editor schema has: StarterKit + attachmentImage; Mermaid is a
// codeBlock with language "mermaid".
const marks = (m = []) => (m.length ? { marks: m.map(type => (typeof type === 'string' ? { type } : type)) } : {})

export const t = (text, ...m) => ({ type: 'text', text, ...marks(m) })
export const link = (text, href) => t(text, { type: 'link', attrs: { href } })
const inl = x => (typeof x === 'string' ? t(x) : x)
export const p = (...c) => ({ type: 'paragraph', content: c.map(inl) })
export const h = (level, text) => ({ type: 'heading', attrs: { level }, content: [t(text)] })
export const ul = (...items) => ({ type: 'bulletList', content: items.map(i => ({ type: 'listItem', content: [Array.isArray(i) ? p(...i) : p(i)] })) })
export const ol = (...items) => ({ type: 'orderedList', content: items.map(i => ({ type: 'listItem', content: [Array.isArray(i) ? p(...i) : p(i)] })) })
export const quote = (...c) => ({ type: 'blockquote', content: [p(...c)] })
export const hr = () => ({ type: 'horizontalRule' })
export const code = (text, language = null) => ({ type: 'codeBlock', attrs: { language }, content: [t(text)] })
export const mermaid = text => code(text, 'mermaid')
export const image = (attachmentId, alt) => ({ type: 'attachmentImage', attrs: { attachmentId, alt } })
export const doc = (...c) => ({ type: 'doc', content: c })
