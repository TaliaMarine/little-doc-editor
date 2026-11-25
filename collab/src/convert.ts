// Conversions between page content (ProseMirror JSON, `pages.content`) and
// Yjs documents, plus the plain-text extraction that feeds the search index.
//
// Ownership (decision): collab owns the Yjs -> ProseMirror JSON conversion.
// It is the only service that links the Tiptap schema and y-tiptap, so on
// every store it derives the JSON itself and writes pages.content and
// pages.search_vector (see persistence.ts). api stays CRDT-agnostic
// (Decision 1) and keeps its own Go SearchIndexer for manual content writes.
import * as Y from 'yjs'
import type { NodeType, Schema } from '@tiptap/pm/model'
import {
  prosemirrorJSONToYXmlFragment,
  yXmlFragmentToProseMirrorRootNode,
} from '@tiptap/y-tiptap'
import { schema as defaultSchema, YDOC_FIELD } from './schema.js'

export type JSONNode = {
  type?: string
  text?: string
  attrs?: Record<string, unknown>
  marks?: { type: string, attrs?: Record<string, unknown> }[]
  content?: JSONNode[]
}

export const EMPTY_DOC: JSONNode = { type: 'doc', content: [{ type: 'paragraph' }] }

// pages.content defaults to '{}' (and may be null-ish): "no content yet".
export function isBlankContent (content: unknown): boolean {
  if (!content || typeof content !== 'object') return true
  const node = content as JSONNode
  if (node.type !== 'doc') return Object.keys(node).length === 0
  return !node.content || node.content.length === 0
}

// Makes arbitrary stored JSON safe for schema.nodeFromJSON: unknown node
// types are unwrapped (their children are kept, the node itself dropped; a
// leaf unknown node disappears), unknown marks are dropped. This is what
// stops a node type the server doesn't know (older/newer frontend, hand-
// edited data) from crashing the seed with "Unknown node type".
export function sanitize (node: JSONNode, schema: Schema = defaultSchema): JSONNode[] {
  if (!node || typeof node !== 'object') return []
  if (node.type === 'text') {
    if (typeof node.text !== 'string' || node.text === '') return []
    const marks = (node.marks ?? []).filter(m => m && typeof m.type === 'string' && schema.marks[m.type])
    return [{ ...node, marks: marks.length ? marks : undefined }]
  }
  const children = (node.content ?? []).flatMap(c => sanitize(c, schema))
  if (!node.type || !schema.nodes[node.type]) {
    return children
  }
  return [{ ...node, content: fitChildren(schema.nodes[node.type], children, schema) }]
}

// Keeps only children that can legally follow each other inside `parent`:
// wraps a misplaced child where the schema offers a wrapping (loose text in a
// doc becomes a paragraph), drops it otherwise.
function fitChildren (parent: NodeType, children: JSONNode[], schema: Schema): JSONNode[] | undefined {
  let match = parent.contentMatch
  const out: JSONNode[] = []
  for (const child of children) {
    const type = schema.nodes[child.type!] ?? schema.nodes.text
    const next = match.matchType(type)
    if (next) {
      out.push(child)
      match = next
      continue
    }
    const wrap = match.findWrapping(type)
    if (!wrap) continue
    let wrapped: JSONNode = child
    for (let i = wrap.length - 1; i >= 0; i--) {
      wrapped = { type: wrap[i].name, content: [wrapped] }
    }
    const afterWrap = match.matchType(wrap[0])
    if (afterWrap) {
      out.push(wrapped)
      match = afterWrap
    }
  }
  return out.length ? out : undefined
}

function nodeFromContent (content: unknown, schema: Schema) {
  const root = sanitize(content as JSONNode, schema)[0]
  if (!root || root.type !== 'doc') {
    throw new Error('content is not a ProseMirror doc')
  }
  const node = schema.nodeFromJSON(root)
  try {
    node.check()
    return node
  } catch {
    // Structurally invalid for the schema (e.g. text directly under doc).
    // Let ProseMirror repair what it can by re-filling required content.
    const filled = schema.topNodeType.createAndFill(node.attrs, node.content)
    if (!filled) throw new Error('content does not fit the schema')
    filled.check()
    return filled
  }
}

// Builds a Yjs document holding `content`. The client ID is fixed so that two
// replicas seeding the same page at the same moment produce byte-identical
// updates — merging them is idempotent instead of duplicating the content.
export function contentToYDoc (content: unknown, schema: Schema = defaultSchema): Y.Doc {
  const doc = new Y.Doc()
  doc.clientID = 1
  if (!isBlankContent(content)) {
    const node = nodeFromContent(content, schema)
    prosemirrorJSONToYXmlFragment(schema, node.toJSON(), doc.getXmlFragment(YDOC_FIELD))
  }
  return doc
}

// Replaces the live fragment's content in one transaction (used when a manual
// content write must override live state). Unlike seeding, uses the doc's own
// client ID: these are real edits that must not collide with the seed's.
export function replaceFragmentContent (doc: Y.Doc, content: unknown, schema: Schema = defaultSchema): void {
  const node = isBlankContent(content) ? null : nodeFromContent(content, schema)
  doc.transact(() => {
    const fragment = doc.getXmlFragment(YDOC_FIELD)
    fragment.delete(0, fragment.length)
    if (node) prosemirrorJSONToYXmlFragment(schema, node.toJSON(), fragment)
  }, 'external-content')
}

// y-tiptap silently drops nodes/marks the schema doesn't know. For write-back
// that would delete content (e.g. a node type from a newer frontend), so refuse
// to derive JSON from such a document instead.
function assertKnown (node: Y.XmlFragment | Y.XmlElement | Y.XmlText | Y.XmlHook, schema: Schema): void {
  if (node instanceof Y.XmlText) {
    for (const op of node.toDelta()) {
      for (const mark of Object.keys(op.attributes ?? {})) {
        if (!schema.marks[mark]) throw new Error(`unknown mark "${mark}" in Yjs document`)
      }
    }
  } else if (node instanceof Y.XmlElement || node instanceof Y.XmlFragment) {
    if (node instanceof Y.XmlElement && !schema.nodes[node.nodeName]) {
      throw new Error(`unknown node "${node.nodeName}" in Yjs document`)
    }
    node.forEach(child => assertKnown(child, schema))
  } else {
    throw new Error('unsupported Yjs node')
  }
}

export function docToContent (doc: Y.Doc, schema: Schema = defaultSchema): JSONNode {
  const fragment = doc.getXmlFragment(YDOC_FIELD)
  assertKnown(fragment, schema)
  if (fragment.length === 0) return { type: 'doc', content: [{ type: 'paragraph' }] }
  return yXmlFragmentToProseMirrorRootNode(fragment, schema).toJSON() as JSONNode
}

export function stateToContent (state: Uint8Array): JSONNode {
  const doc = new Y.Doc()
  Y.applyUpdate(doc, state)
  return docToContent(doc)
}

// What `content` looks like after one trip through the schema + Yjs. Used to
// tell "stored JSON differs from live state because the schema normalised it"
// apart from "someone else wrote different content".
export function normalizeContent (content: unknown, schema: Schema = defaultSchema): JSONNode {
  return docToContent(contentToYDoc(content, schema), schema)
}

export function canonicalJSON (value: unknown): string {
  return JSON.stringify(value, (_k, v) => {
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      return Object.fromEntries(Object.entries(v).sort(([a], [b]) => (a < b ? -1 : 1)))
    }
    return v
  })
}

export function sameContent (a: unknown, b: unknown): boolean {
  return canonicalJSON(a) === canonicalJSON(b)
}

// Same extraction as api/internal/searchindex.ExtractPlainText: every text
// node's text, depth-first, joined by single spaces, trimmed. Kept
// deliberately identical (including joining across inline marks) so the
// tsvector built here matches what api builds for the same content; the SQL
// side is `to_tsvector('english', $text)` in both places.
export function extractPlainText (content: unknown): string {
  const parts: string[] = []
  const walk = (n: JSONNode) => {
    if (n && typeof n.text === 'string' && n.text !== '') parts.push(n.text)
    for (const c of n?.content ?? []) walk(c)
  }
  walk(content as JSONNode)
  return parts.join(' ').trim()
}
