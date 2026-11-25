<template>
  <v-navigation-drawer
    v-model="visible"
    location="right"
    temporary
    width="360"
  >
    <div class="d-flex align-center pa-3">
      <h2 class="text-subtitle-1 flex-grow-1">{{ t('history.title') }}</h2>

      <v-btn
        :aria-label="t('history.close')"
        icon="mdi-close"
        size="small"
        variant="text"
        @click="visible = false"
      />
    </div>

    <v-alert v-if="page.history_enabled === false" class="mx-3 mb-2" density="compact" type="info">{{ t('history.off') }}</v-alert>
    <v-alert v-if="error" class="mx-3 mb-2" density="compact" type="error">{{ error }}</v-alert>
    <v-progress-linear v-if="loading" indeterminate />
    <p v-else-if="revisions.length === 0 && !error" class="px-4 text-body-2" data-testid="history-empty">{{ t('history.empty') }}</p>

    <v-list v-else data-testid="history-list" lines="two">
      <v-list-item
        v-for="revision in revisions"
        :key="revision.id"
        :data-testid="`revision-${revision.id}`"
        :subtitle="authorOf(revision)"
        :title="formatDate(revision.created_at)"
        @click="open(revision)"
      >
        <template v-if="revision.title" #append>
          <span class="text-caption text-medium-emphasis text-truncate" style="max-width: 120px">{{ revision.title }}</span>
        </template>
      </v-list-item>
    </v-list>
  </v-navigation-drawer>

  <v-dialog v-model="previewOpen" max-width="860" scrollable>
    <v-card data-testid="revision-preview" :title="t('history.preview', { when: previewRevision ? formatDate(previewRevision.created_at) : '' })">
      <v-card-text>
        <v-alert v-if="previewError" class="mb-3" density="compact" type="error">{{ previewError }}</v-alert>
        <v-progress-linear v-if="previewLoading" indeterminate />
        <h3 v-if="previewTitle" class="text-h6 mb-2">{{ previewTitle }}</h3>

        <div v-show="!previewLoading && !previewError" class="revision-surface" data-testid="revision-content">
          <editor-content v-if="previewEditor" :editor="previewEditor" />
        </div>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn @click="previewOpen = false">{{ t('history.close') }}</v-btn>
        <!-- Restore is not rendered at all unless the caller is editor+ and
             the page is unlocked (Decision 30); the api enforces it too. -->
        <v-btn
          v-if="canRestore"
          color="primary"
          data-testid="revision-restore"
          :disabled="previewLoading || !!previewError"
          @click="confirmOpen = true"
        >{{ t('history.restore') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>

  <v-dialog v-if="canRestore" v-model="confirmOpen" max-width="440">
    <v-card :title="t('history.restoreTitle')">
      <v-card-text>
        <p>{{ t('history.restoreBody') }}</p>
        <v-alert v-if="restoreError" class="mt-3" density="compact" type="error">{{ restoreError }}</v-alert>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn :disabled="restoring" @click="confirmOpen = false">{{ t('history.cancel') }}</v-btn>

        <v-btn
          color="primary"
          data-testid="revision-restore-confirm"
          :loading="restoring"
          variant="flat"
          @click="restore"
        >{{ t('history.restore') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import type { Page, PageRevision } from '@/api/generated'
  import { Editor, EditorContent } from '@tiptap/vue-3'
  import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { getPageRevision, listPageRevisions, restorePageRevision } from '@/api/generated'
  import { createExtensions } from '@/editor/extensions'
  import { canRestoreRevision } from '@/utils/permissions'

  // Members only (viewer+, Decisions 30/42): the parent mounts this solely
  // when canViewHistory(page); restore is a further editor+ gate below.
  const props = defineProps<{ page: Pick<Page, 'id' | 'my_role' | 'locked' | 'history_enabled'> }>()
  const visible = defineModel<boolean>({ required: true })
  const emit = defineEmits<{ restored: [] }>()

  const { t } = useI18n()

  const revisions = ref<PageRevision[]>([])
  const loading = ref(false)
  const error = ref('')

  const canRestore = computed(() => canRestoreRevision(props.page))

  function formatDate (iso: string) {
    return new Date(iso).toLocaleString()
  }

  function authorOf (revision: PageRevision) {
    return revision.author_user_display_name
      ?? (revision.author_guest_display_name ? t('history.guest', { name: revision.author_guest_display_name }) : t('history.unknownAuthor'))
  }

  async function load () {
    loading.value = true
    error.value = ''
    try {
      const { data, error: apiError } = await listPageRevisions({ path: { pageId: props.page.id } })
      if (apiError || !data) {
        throw new Error(apiError?.error ?? t('history.loadFailed'))
      }
      revisions.value = data
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('history.loadFailed')
    } finally {
      loading.value = false
    }
  }

  watch(visible, isOpen => {
    if (isOpen) {
      load()
    }
  }, { immediate: true })

  // --- preview: a read-only render of the revision's content ---

  const previewOpen = ref(false)
  const previewRevision = ref<PageRevision | null>(null)
  const previewTitle = ref('')
  const previewLoading = ref(false)
  const previewError = ref('')
  const previewEditor = shallowRef<Editor | null>(null)

  function destroyPreview () {
    previewEditor.value?.destroy()
    previewEditor.value = null
  }

  watch(previewOpen, open => {
    if (!open) {
      destroyPreview()
    }
  })
  onBeforeUnmount(destroyPreview)

  async function open (revision: PageRevision) {
    destroyPreview()
    previewRevision.value = revision
    previewTitle.value = revision.title ?? ''
    previewError.value = ''
    previewLoading.value = true
    previewOpen.value = true
    try {
      const { data, error: apiError } = await getPageRevision({ path: { pageId: props.page.id, revisionId: revision.id } })
      if (apiError || !data) {
        throw new Error(apiError?.error ?? t('history.loadFailed'))
      }
      if (previewRevision.value?.id !== revision.id) {
        return
      }
      previewTitle.value = data.title ?? ''
      previewEditor.value = new Editor({ extensions: createExtensions(), content: data.content, editable: false })
    } catch (error_) {
      previewError.value = error_ instanceof Error ? error_.message : t('history.loadFailed')
    } finally {
      previewLoading.value = false
    }
  }

  // --- restore ---

  const confirmOpen = ref(false)
  const restoring = ref(false)
  const restoreError = ref('')

  async function restore () {
    if (!previewRevision.value || !canRestore.value) {
      return
    }
    restoring.value = true
    restoreError.value = ''
    try {
      const { error: apiError } = await restorePageRevision({ path: { pageId: props.page.id, revisionId: previewRevision.value.id } })
      if (apiError) {
        throw new Error(apiError.error ?? t('history.restoreFailed'))
      }
      confirmOpen.value = false
      previewOpen.value = false
      visible.value = false
      emit('restored')
    } catch (error_) {
      restoreError.value = error_ instanceof Error ? error_.message : t('history.restoreFailed')
    } finally {
      restoring.value = false
    }
  }
</script>

<style scoped>
.revision-surface { border: 1px solid rgba(0, 0, 0, 0.12); border-radius: 4px; padding: 16px; min-height: 120px; }
.revision-surface :deep(.ProseMirror) { outline: none; }
</style>
