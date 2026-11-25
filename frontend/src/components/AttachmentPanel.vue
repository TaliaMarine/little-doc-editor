<template>
  <section aria-labelledby="attachments-heading" class="attachment-panel">
    <h2 id="attachments-heading" class="text-subtitle-1 mb-2">{{ t('attachments.title') }}</h2>

    <div
      v-if="canUpload"
      class="attachment-panel__drop"
      :class="{ 'is-over': dragging }"
      data-testid="dropzone"
      @dragenter.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @dragover.prevent
      @drop.prevent="onDrop"
    >
      <v-icon class="mr-2" icon="mdi-paperclip" />
      <span class="mr-3">{{ t('attachments.dropHint') }}</span>
      <v-btn size="small" variant="tonal" @click="picker?.click()">{{ t('attachments.choose') }}</v-btn>

      <input
        ref="picker"
        class="d-none"
        data-testid="file-input"
        multiple
        type="file"
        @change="onPick"
      >
    </div>

    <v-list v-if="store.uploads.length > 0" density="compact" lines="two">
      <v-list-item v-for="item in store.uploads" :key="item.id" :title="item.filename">
        <template #subtitle>
          <v-progress-linear
            v-if="item.status === 'uploading'"
            :aria-label="t('attachments.uploading')"
            class="mt-1"
            :model-value="item.progress * 100"
          />
          <!-- Server message as-is (422 policy rejections). -->
          <span v-else class="text-error" role="alert">{{ item.error }}</span>
        </template>

        <template v-if="item.status === 'error'" #append>
          <v-btn
            :aria-label="t('attachments.dismiss')"
            icon="mdi-close"
            size="small"
            variant="text"
            @click="store.dismissUpload(item.id)"
          />
        </template>
      </v-list-item>
    </v-list>

    <v-alert v-if="store.loadError" density="compact" type="error">{{ store.loadError }}</v-alert>

    <p v-else-if="!store.loading && store.items.length === 0 && store.uploads.length === 0" class="text-body-2 text-medium-emphasis">
      {{ t('attachments.empty') }}
    </p>

    <v-list v-if="store.items.length > 0" density="compact" lines="two">
      <v-list-item v-for="item in store.items" :key="item.id" data-testid="attachment-item">
        <template #prepend>
          <AttachmentThumb :attachment="item" class="mr-3" />
        </template>

        <v-list-item-title>{{ item.filename }}</v-list-item-title>
        <v-list-item-subtitle>{{ formatSize(item.size_bytes) }} · {{ item.content_type }}</v-list-item-subtitle>

        <template #append>
          <v-btn
            v-if="canUpload && (canInsert ?? true) && isImageType(item.content_type)"
            :aria-label="t('attachments.insert')"
            icon="mdi-image-plus"
            :title="t('attachments.insert')"
            variant="text"
            @click="emit('insert-image', item)"
          />

          <v-btn
            v-if="isPdfType(item.content_type)"
            :aria-label="t('attachments.view')"
            icon="mdi-file-eye-outline"
            :title="t('attachments.view')"
            variant="text"
            @click="viewing = item"
          />

          <v-btn
            :aria-label="t('attachments.download')"
            icon="mdi-download"
            :loading="downloading === item.id"
            :title="t('attachments.download')"
            variant="text"
            @click="download(item)"
          />
        </template>
      </v-list-item>
    </v-list>

    <PdfViewer v-if="viewing" :attachment="viewing" @update:model-value="viewing = null" />

    <v-snackbar v-model="downloadFailed" color="error" timeout="4000">{{ t('attachments.downloadFailed') }}</v-snackbar>
  </section>
</template>

<script lang="ts" setup>
  import type { Page } from '@/stores/pages'
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import AttachmentThumb from '@/components/AttachmentThumb.vue'
  import PdfViewer from '@/components/PdfViewer.vue'
  import { type Attachment, useAttachmentsStore } from '@/stores/attachments'
  import { attachmentBlobCache } from '@/utils/attachmentBlobCache'
  import { formatSize, isImageType, isPdfType } from '@/utils/attachments'
  import { canEditPage } from '@/utils/permissions'

  const props = defineProps<{ page: Pick<Page, 'id' | 'can_edit' | 'locked'>, canInsert?: boolean }>()
  const emit = defineEmits<{ 'insert-image': [attachment: Attachment] }>()

  const { t } = useI18n()
  const store = useAttachmentsStore()
  // Upload controls are hidden (not disabled) for callers who can't edit.
  const canUpload = computed(() => canEditPage(props.page))

  const picker = ref<HTMLInputElement | null>(null)
  const dragging = ref(false)
  const viewing = ref<Attachment | null>(null)
  const downloading = ref<string | null>(null)
  const downloadFailed = ref(false)

  watch(() => props.page.id, id => store.fetchFor(id), { immediate: true })

  async function uploadAll (files: File[]) {
    // Sequential (Decision 32 style): one request per file, failures are
    // shown per file by the store and don't stop the rest.
    for (const file of files) {
      try {
        await store.upload(props.page.id, file)
      } catch {
        // surfaced via store.uploads
      }
    }
  }

  function onPick (event: Event) {
    const input = event.target as HTMLInputElement
    const files = [...(input.files ?? [])]
    input.value = ''
    uploadAll(files)
  }

  function onDrop (event: DragEvent) {
    dragging.value = false
    uploadAll([...(event.dataTransfer?.files ?? [])])
  }

  // Authenticated download: bytes via the api client -> blob URL -> <a download>.
  async function download (item: Attachment) {
    downloading.value = item.id
    try {
      const url = await attachmentBlobCache.acquire(item.id)
      const link = document.createElement('a')
      link.href = url
      link.download = item.filename
      document.body.append(link)
      link.click()
      link.remove()
    } catch {
      downloadFailed.value = true
    } finally {
      attachmentBlobCache.release(item.id)
      downloading.value = null
    }
  }
</script>

<style scoped>
.attachment-panel { margin-top: 24px; }
.attachment-panel__drop {
  display: flex; align-items: center; padding: 12px 16px; margin-bottom: 8px;
  border: 2px dashed rgba(127, 127, 127, 0.5); border-radius: 4px;
}
.attachment-panel__drop.is-over { border-color: rgb(var(--v-theme-primary)); background: rgba(var(--v-theme-primary), 0.08); }
</style>
