<template>
  <node-view-wrapper as="span" class="attachment-image" :class="{ 'is-selected': selected }" data-drag-handle>
    <img
      v-if="url"
      :alt="node.attrs.alt ?? ''"
      class="attachment-image__img"
      draggable="false"
      :src="url"
    >

    <span v-else-if="error" :aria-label="t('attachments.imageFailed')" class="attachment-image__placeholder text-error" role="img">
      <v-icon icon="mdi-image-broken-variant" size="small" />
      {{ t('attachments.imageFailed') }}
    </span>

    <span v-else :aria-label="t('attachments.imageLoading')" class="attachment-image__placeholder" role="img">
      <v-progress-circular indeterminate size="16" width="2" />
    </span>
  </node-view-wrapper>
</template>

<script lang="ts" setup>
  import type { NodeViewProps } from '@tiptap/vue-3'
  import { NodeViewWrapper } from '@tiptap/vue-3'
  import { useI18n } from 'vue-i18n'
  import { useAttachmentUrl } from '@/composables/useAttachmentUrl'

  const props = defineProps<NodeViewProps>()
  const { t } = useI18n()
  const { url, error } = useAttachmentUrl(() => props.node.attrs.attachmentId as string | null)
</script>

<style scoped>
.attachment-image { display: inline-block; max-width: 100%; vertical-align: bottom; }
.attachment-image__img { max-width: 100%; max-height: 480px; display: block; }
.attachment-image.is-selected { outline: 2px solid rgb(var(--v-theme-primary)); }
.attachment-image__placeholder { display: inline-flex; align-items: center; gap: 4px; min-width: 32px; min-height: 24px; }
</style>
