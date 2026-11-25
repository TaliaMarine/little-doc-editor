// Lazy mermaid loader — the package is large, so it's only fetched when a
// document actually contains a ```mermaid block. securityLevel 'strict'
// makes mermaid sanitize the diagram text/SVG (no click handlers or raw HTML
// labels), which matters because diagrams can come from other collaborators.
import type Mermaid from 'mermaid'
import { sanitizeSvg } from '@/utils/sanitizeSvg'

type MermaidApi = typeof Mermaid

let loading: Promise<MermaidApi> | null = null
let counter = 0

function load (): Promise<MermaidApi> {
  loading ??= import('mermaid').then(module => {
    const mermaid = module.default
    return mermaid
  })
  return loading
}

export type MermaidResult = { svg: string } | { error: string }

// `dark` picks mermaid's dark theme so diagrams follow the app theme.
export async function renderMermaid (source: string, dark = false): Promise<MermaidResult> {
  const id = `mermaid-render-${++counter}`
  try {
    const mermaid = await load()
    mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: dark ? 'dark' : 'default' })
    const { svg } = await mermaid.render(id, source)
    // Rendered through v-html: sanitize again on top of mermaid's own strict mode.
    return { svg: sanitizeSvg(svg) }
  } catch (error) {
    // mermaid leaves its error graphic in the DOM on failure.
    document.querySelector(`#d${id}`)?.remove()
    document.querySelector(`#${id}`)?.remove()
    return { error: error instanceof Error ? error.message : String(error) }
  }
}
