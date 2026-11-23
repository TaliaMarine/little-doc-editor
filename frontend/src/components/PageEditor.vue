<template>
  <div>
    <v-text-field
      v-model="title"
      class="mb-2"
      :label="t('editor.titleLabel')"
      :readonly="!editable"
      variant="underlined"
    />

    <v-alert v-if="page.locked" class="mb-2" density="compact" type="info">{{ t('editor.locked') }}</v-alert>
    <v-alert v-else-if="!page.can_edit" class="mb-2" density="compact" type="info">{{ t('editor.readOnly') }}</v-alert>

    <v-alert
      v-if="errorMessage"
      class="mb-2"
      closable
      density="compact"
      type="error"
      @click:close="errorMessage = ''"
    >
      {{ errorMessage }}
    </v-alert>

    <div class="editor-surface">
      <editor-content :editor="editor" />
    </div>

    <v-btn
      v-if="editable"
      class="mt-3"
      color="primary"
      :loading="saving"
      @click="save"
    >
      {{ t('editor.save') }}
    </v-btn>

    <v-snackbar v-model="showSaved" timeout="2000">{{ t('editor.saved') }}</v-snackbar>
  </div>
</template>

<script lang="ts" setup>
  import StarterKit from '@tiptap/starter-kit'
  import { Editor, EditorContent } from '@tiptap/vue-3'
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { type Page, usePagesStore } from '@/stores/pages'
  import { canEditPage } from '@/utils/permissions'

  // No live collaboration yet (SPEC.md §5.1 build order defers the Yjs
  // extension to a follow-up slice) — this is solo editing with manual save
  // against PUT /pages/{id}/content.
  const props = defineProps<{ page: Page }>()

  const { t } = useI18n()
  const pagesStore = usePagesStore()

  // Hide (not just disable) what the caller can't do — viewers, view-link
  // guests, and everyone on a locked page get a read-only view. The server
  // enforces this regardless; this just stops offering a Save that 403s.
  // Reactive so a same-page update (e.g. live collab, NEXT.md §5) that
  // locks the page or changes access takes effect without a remount.
  const editable = computed(() => canEditPage(props.page))

  const title = ref(props.page.title)
  const saving = ref(false)
  const errorMessage = ref('')
  const showSaved = ref(false)

  const editor = new Editor({
    extensions: [StarterKit],
    content: props.page.content,
    editable: editable.value,
  })
  watch(editable, value => editor.setEditable(value))

  onBeforeUnmount(() => editor.destroy())

  async function save () {
    saving.value = true
    errorMessage.value = ''
    try {
      await pagesStore.savePage(props.page.id, title.value, editor.getJSON())
      showSaved.value = true
    } catch (error) {
      // A 403 here means the server disagrees with what the UI assumed
      // (SPEC.md's permission model is server-enforced, not UI-only) —
      // surfacing the real error is correct, not a bug to paper over.
      errorMessage.value = error instanceof Error ? error.message : t('editor.saveFailed')
    } finally {
      saving.value = false
    }
  }
</script>

<style scoped>
.editor-surface {
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 4px;
  padding: 16px;
  min-height: 200px;
}
</style>
