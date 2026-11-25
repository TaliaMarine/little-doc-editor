<template>
  <v-dialog v-model="open" fullscreen scrollable>
    <v-card>
      <v-toolbar density="compact">
        <v-toolbar-title class="text-body-1">{{ attachment.filename }}</v-toolbar-title>
        <v-spacer />
        <v-btn :aria-label="t('attachments.close')" icon="mdi-close" @click="open = false" />
      </v-toolbar>

      <v-card-text class="pdf-viewer__body">
        <v-alert v-if="error" density="compact" type="error">{{ t('attachments.pdfFailed') }}</v-alert>
        <v-progress-linear v-else-if="!url" indeterminate />
        <PdfEmbed v-else :source="url" text-layer @loading-failed="failed = true" />
        <v-alert v-if="failed" density="compact" type="error">{{ t('attachments.pdfFailed') }}</v-alert>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import type { Attachment } from '@/stores/attachments'
  import { defineAsyncComponent, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useAttachmentUrl } from '@/composables/useAttachmentUrl'

  // pdf.js is large: only fetched when someone opens a PDF.
  const PdfEmbed = defineAsyncComponent(async () => {
    await import('vue-pdf-embed/dist/styles/textLayer.css')
    // The default vue-pdf-embed entry inlines pdf.js's worker as a data: URL,
    // which the SPA's CSP (script-src/worker-src 'self' blob:) blocks. Use the
    // "essential" build and serve the worker as a normal hashed asset.
    const [{ default: component, GlobalWorkerOptions }, { default: workerSrc }] = await Promise.all([
      import('vue-pdf-embed/dist/index.essential.mjs'),
      import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'),
    ])
    GlobalWorkerOptions.workerSrc = workerSrc
    return component
  })

  const props = defineProps<{ attachment: Attachment }>()
  const open = defineModel<boolean>({ default: true })
  const { t } = useI18n()
  const failed = ref(false)
  // Held only while the viewer is mounted (the parent v-ifs it).
  const { url, error } = useAttachmentUrl(() => props.attachment.id)
</script>

<style scoped>
.pdf-viewer__body { background: rgba(127, 127, 127, 0.15); }
</style>
