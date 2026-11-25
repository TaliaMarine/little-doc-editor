<template>
  <v-img
    v-if="url"
    :alt="attachment.filename"
    aspect-ratio="1"
    class="rounded"
    cover
    :src="url"
    width="56"
  />

  <v-avatar v-else color="surface-variant" rounded size="56">
    <v-icon :icon="isPdfType(attachment.content_type) ? 'mdi-file-pdf-box' : 'mdi-file-outline'" />
  </v-avatar>
</template>

<script lang="ts" setup>
  import type { Attachment } from '@/stores/attachments'
  import { useAttachmentUrl } from '@/composables/useAttachmentUrl'
  import { isImageType, isPdfType } from '@/utils/attachments'

  const props = defineProps<{ attachment: Attachment }>()
  // Only images need bytes for a thumbnail; other types show an icon.
  const { url } = useAttachmentUrl(() => isImageType(props.attachment.content_type) ? props.attachment.id : null)
</script>
