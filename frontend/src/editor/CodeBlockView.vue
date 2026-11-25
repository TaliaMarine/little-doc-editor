<template>
  <node-view-wrapper class="code-block" :class="{ 'code-block--mermaid': isMermaid }">
    <div v-if="isMermaid" class="code-block__toolbar" contenteditable="false">
      <span class="text-caption text-medium-emphasis">{{ t('mermaid.label') }}</span>

      <v-btn-toggle
        v-model="mode"
        density="compact"
        divided
        mandatory
        variant="outlined"
      >
        <v-btn size="x-small" value="preview">{{ t('mermaid.preview') }}</v-btn>
        <v-btn size="x-small" value="edit">{{ editable ? t('mermaid.edit') : t('mermaid.source') }}</v-btn>
      </v-btn-toggle>
    </div>

    <!-- The editable code must stay mounted (hidden, not removed) in preview
         mode: ProseMirror owns this contentDOM, and unmounting it would detach
         the node's text from the view. -->
    <pre v-show="!isMermaid || mode === 'edit'" class="code-block__pre"><code><node-view-content as="span" /></code></pre>

    <div v-if="isMermaid && mode === 'preview'" class="code-block__preview" contenteditable="false">
      <v-alert v-if="result && 'error' in result" density="compact" type="error">
        <div class="text-caption">{{ t('mermaid.error') }}</div>
        <pre class="code-block__error">{{ result.error }}</pre>
      </v-alert>
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div v-else-if="result" class="code-block__svg" v-html="result.svg" />
      <v-progress-linear v-else-if="source.trim()" indeterminate />
      <div v-else class="text-caption text-medium-emphasis">{{ t('mermaid.empty') }}</div>
    </div>
  </node-view-wrapper>
</template>

<script lang="ts" setup>
  import type { MermaidResult } from './mermaidRender'
  import type { NodeViewProps } from '@tiptap/vue-3'
  import { NodeViewContent, NodeViewWrapper } from '@tiptap/vue-3'
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useTheme } from 'vuetify'
  import { renderMermaid } from './mermaidRender'

  const props = defineProps<NodeViewProps>()
  const { t } = useI18n()
  const theme = useTheme()
  const isDark = computed(() => theme.current.value.dark)

  const isMermaid = computed(() => props.node.attrs.language === 'mermaid')
  const source = computed(() => props.node.textContent)
  // editor.isEditable isn't reactive; refresh it on every transaction
  // (setEditable dispatches one).
  const editable = ref(props.editor.isEditable)
  function syncEditable () {
    editable.value = props.editor.isEditable
  }
  props.editor.on('transaction', syncEditable)

  // Local view state — never written to the document, so toggling is Yjs-safe
  // and per-user. A fresh, empty block starts in edit mode so it can be typed in.
  const mode = ref<'edit' | 'preview'>(props.editor.isEditable && !props.node.textContent.trim() ? 'edit' : 'preview')
  const result = ref<MermaidResult | null>(null)

  let timer: ReturnType<typeof setTimeout> | undefined
  let generation = 0

  // Debounced so remote keystrokes / typing don't re-render on every character.
  watch([source, isMermaid, mode, isDark], () => {
    clearTimeout(timer)
    if (!isMermaid.value || mode.value !== 'preview') return
    if (!source.value.trim()) {
      result.value = null
      return
    }
    const current = ++generation
    timer = setTimeout(async () => {
      const rendered = await renderMermaid(source.value, isDark.value)
      if (current === generation) result.value = rendered
    }, 250)
  }, { immediate: true })

  onBeforeUnmount(() => {
    props.editor.off('transaction', syncEditable)
    clearTimeout(timer)
    generation++
  })
</script>

<style scoped>
.code-block { margin: 0.5rem 0; }
.code-block__toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.code-block__pre { background: rgba(127, 127, 127, 0.12); border-radius: 4px; padding: 8px 12px; overflow-x: auto; margin: 0; }
.code-block__preview { padding: 8px; overflow-x: auto; text-align: center; }
.code-block__svg :deep(svg) { max-width: 100%; height: auto; }
.code-block__error { white-space: pre-wrap; text-align: left; }
</style>
