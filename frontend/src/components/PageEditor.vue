<template>
  <div>
    <v-text-field
      v-model="title"
      class="mb-2"
      :error-messages="titleError"
      :label="t('editor.titleLabel')"
      :loading="savingTitle"
      :readonly="!editable"
      variant="underlined"
      @blur="saveTitle"
      @keydown.enter.prevent="saveTitle"
    />

    <v-alert v-if="page.locked" class="mb-2" density="compact" type="info">{{ t('editor.locked') }}</v-alert>
    <v-alert v-else-if="!page.can_edit || collab.serverReadOnly.value" class="mb-2" density="compact" type="info">{{ t('editor.readOnly') }}</v-alert>

    <!-- Live-session status: always visible so unsynced edits are never silent. -->
    <div class="d-flex align-center flex-wrap ga-2 mb-2" data-testid="collab-status">
      <v-chip :color="statusColor" size="small" variant="tonal">
        <v-icon :icon="statusIcon" start />
        {{ statusText }}
      </v-chip>

      <v-chip v-if="collab.unsynced.value > 0" color="warning" size="small" variant="tonal">
        {{ t('collab.unsynced', collab.unsynced.value) }}
      </v-chip>

      <v-spacer />

      <!-- Member-only tools: never offered to share-link guests (Decisions 30, 42). -->
      <template v-if="!guest">
        <v-btn
          v-if="canViewHistory(page)"
          data-testid="open-history"
          prepend-icon="mdi-history"
          size="small"
          variant="text"
          @click="historyOpen = true"
        >{{ t('history.title') }}</v-btn>

        <v-btn
          v-if="canManageShareLinks(page)"
          data-testid="open-share"
          prepend-icon="mdi-share-variant-outline"
          size="small"
          variant="text"
          @click="shareOpen = true"
        >{{ t('shareLink.button') }}</v-btn>
      </template>

      <div aria-live="polite" class="d-flex align-center ga-1" data-testid="presence">
        <v-tooltip v-for="person in collab.people.value" :key="person.clientId" location="bottom" :text="person.self ? t('collab.you', { name: person.name }) : person.name">
          <template #activator="{ props: tipProps }">
            <v-avatar v-bind="tipProps" :color="person.color" size="28">
              <span class="text-caption text-white">{{ person.name.slice(0, 1).toUpperCase() }}</span>
            </v-avatar>
          </template>
        </v-tooltip>
      </div>
    </div>

    <v-alert
      v-if="collab.unavailable.value"
      class="mb-2"
      density="compact"
      type="warning"
    >
      {{ collab.connection.value === 'failed' ? t('collab.failed', { reason: collab.failureReason.value }) : t('collab.unavailable') }}
      <template #append>
        <v-btn size="small" variant="outlined" @click="collab.retry()">{{ t('collab.retry') }}</v-btn>
      </template>
    </v-alert>

    <v-alert
      v-else-if="collab.everSynced.value && collab.connection.value !== 'connected'"
      class="mb-2"
      density="compact"
      type="warning"
    >
      {{ t('collab.offlineKept') }}
    </v-alert>

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

    <div
      v-if="editable"
      :aria-label="t('editor.toolbar')"
      aria-orientation="horizontal"
      class="editor-toolbar mb-1"
      role="toolbar"
    >
      <v-btn
        :aria-label="t('editor.bold')"
        icon="mdi-format-bold"
        size="small"
        variant="text"
        @click="editor.chain().focus().toggleBold().run()"
      />

      <v-btn
        :aria-label="t('editor.italic')"
        icon="mdi-format-italic"
        size="small"
        variant="text"
        @click="editor.chain().focus().toggleItalic().run()"
      />

      <v-btn
        :aria-label="t('editor.bulletList')"
        icon="mdi-format-list-bulleted"
        size="small"
        variant="text"
        @click="editor.chain().focus().toggleBulletList().run()"
      />

      <v-btn
        :aria-label="t('editor.codeBlock')"
        icon="mdi-code-braces"
        size="small"
        variant="text"
        @click="editor.chain().focus().toggleCodeBlock().run()"
      />

      <v-btn
        :aria-label="t('editor.insertDiagram')"
        icon="mdi-graph-outline"
        size="small"
        :title="t('editor.insertDiagram')"
        variant="text"
        @click="insertDiagram"
      />
    </div>

    <v-progress-linear v-if="!collab.everSynced.value && !collab.unavailable.value" class="mb-2" indeterminate />

    <div v-show="showCollabEditor" class="editor-surface" data-testid="editor">
      <editor-content :editor="editor" />
    </div>

    <div v-if="fallbackEditor" class="editor-surface" data-testid="editor-fallback">
      <editor-content :editor="fallbackEditor" />
    </div>

    <template v-if="!guest">
      <PageHistoryDrawer v-if="canViewHistory(page)" v-model="historyOpen" :page="page" @restored="onRestored" />
      <ShareLinkDialog v-if="canManageShareLinks(page)" v-model="shareOpen" :page-id="page.id" />
    </template>

    <AttachmentPanel :can-insert="editable" :page="page" @insert-image="insertImage" />

    <v-snackbar v-model="showSaved" timeout="2000">{{ t('editor.saved') }}</v-snackbar>
  </div>
</template>

<script lang="ts" setup>
  import { Editor, EditorContent } from '@tiptap/vue-3'
  import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import AttachmentPanel from '@/components/AttachmentPanel.vue'
  import PageHistoryDrawer from '@/components/PageHistoryDrawer.vue'
  import ShareLinkDialog from '@/components/ShareLinkDialog.vue'
  import { useCollabSession } from '@/composables/useCollabSession'
  import { createExtensions } from '@/editor/extensions'
  import { type Attachment, useAttachmentsStore } from '@/stores/attachments'
  import { type Page, usePagesStore } from '@/stores/pages'
  import { canEditPage, canManageShareLinks, canViewHistory } from '@/utils/permissions'

  // Live collaboration (NEXT.md §4): the Yjs document on the collab service is
  // the source of truth while a page is open, and collab writes it back to
  // pages.content. There is therefore no manual Save for content; the title is
  // saved separately on blur. If the collab connection never comes up, the
  // last persisted content is shown read-only rather than a second, unsynced
  // editing path.
  //
  // `guest` is set on the /share route: the session comes from the redeemed
  // share link (token + the name from the mandatory prompt), and history/share
  // tools are not offered.
  const props = defineProps<{ page: Page, guest?: { token: string, displayName: string } }>()

  const { t } = useI18n()
  const pagesStore = usePagesStore()
  const attachmentsStore = useAttachmentsStore()
  const collab = useCollabSession(props.page.id, props.guest)

  const title = ref(props.page.title)
  const savedTitle = ref(props.page.title)
  const savingTitle = ref(false)
  const titleError = ref('')
  const errorMessage = ref('')
  const showSaved = ref(false)

  // Hide (not just disable) what the caller can't do; the server enforces it
  // regardless. Reactive so lock/role changes apply without a remount. Not
  // editable until the first sync so we never type into a not-yet-seeded doc.
  const editable = computed(() => canEditPage(props.page) && !collab.serverReadOnly.value && collab.everSynced.value)

  const editor = new Editor({
    extensions: createExtensions({
      collab: { document: collab.session.value.document, provider: collab.session.value.provider, user: { ...collab.user } },
      imageUpload: {
        upload: async file => (await attachmentsStore.upload(props.page.id, file)).id,
        uploadFile: file => attachmentsStore.upload(props.page.id, file),
      },
    }),
    editable: false,
  })
  watch(editable, value => editor.setEditable(value), { immediate: true })

  // Read-only view of the persisted content, only when live editing is down.
  const fallbackEditor = shallowRef<Editor | null>(null)
  const showCollabEditor = computed(() => collab.everSynced.value)
  watch(() => collab.unavailable.value, unavailable => {
    if (unavailable && !fallbackEditor.value) {
      fallbackEditor.value = new Editor({ extensions: createExtensions(), content: props.page.content, editable: false })
    } else if (!unavailable && fallbackEditor.value) {
      fallbackEditor.value.destroy()
      fallbackEditor.value = null
    }
  }, { immediate: true })

  onBeforeUnmount(() => {
    editor.destroy()
    fallbackEditor.value?.destroy()
    collab.destroy()
  })

  // Access can change mid-session (collab flips a demoted connection read-only
  // in place without telling the client), so re-read can_edit/locked when the
  // connection state changes, or when edits stay unacknowledged.
  async function refreshAccess () {
    try {
      await pagesStore.fetchPage(props.page.id)
    } catch {
      // A 403/404 here is surfaced by the page view on its next load; the
      // collab connection itself is closed by the server in that case.
    }
  }
  let unmounted = false
  onBeforeUnmount(() => {
    unmounted = true
  })
  watch(() => collab.connection.value, (state, previous) => {
    if (previous !== undefined && !unmounted) refreshAccess()
  })
  let unsyncedTimer: ReturnType<typeof setTimeout> | undefined
  watch(() => collab.unsynced.value, count => {
    clearTimeout(unsyncedTimer)
    if (count > 0) {
      unsyncedTimer = setTimeout(() => {
        if (collab.connection.value === 'connected') refreshAccess()
      }, 6000)
    }
  })
  onBeforeUnmount(() => clearTimeout(unsyncedTimer))

  const historyOpen = ref(false)
  const shareOpen = ref(false)

  // A restore replaces pages.content on the server; api then notifies collab,
  // which merges it into the live room and broadcasts it to every connected
  // client (collab/README.md). Remount with a fresh document anyway: a client
  // that connects before that broadcast still receives it as a live update.
  async function onRestored () {
    await refreshAccess()
    pagesStore.editorEpoch++
  }

  function insertImage (attachment: Attachment) {
    editor.chain().focus().insertContent({ type: 'attachmentImage', attrs: { attachmentId: attachment.id, alt: attachment.filename } }).run()
  }

  function insertDiagram () {
    editor.chain().focus().insertContent({
      type: 'codeBlock',
      attrs: { language: 'mermaid' },
      content: [{ type: 'text', text: 'graph TD\n  A[Start] --> B[End]' }],
    }).run()
  }

  // The title isn't part of the Yjs document; it's saved on its own (title
  // only — never send content while collab owns it).
  async function saveTitle () {
    const next = title.value.trim()
    if (!editable.value || savingTitle.value || next === savedTitle.value) return
    if (!next) {
      titleError.value = t('editor.titleRequired')
      return
    }
    titleError.value = ''
    savingTitle.value = true
    try {
      await pagesStore.renamePage(props.page.id, next)
      savedTitle.value = next
      showSaved.value = true
    } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : t('editor.saveFailed')
    } finally {
      savingTitle.value = false
    }
  }

  const statusColor = computed(() => ({ connected: 'success', connecting: 'info', disconnected: 'warning', failed: 'error' })[collab.connection.value])
  const statusIcon = computed(() => ({ connected: 'mdi-cloud-check-outline', connecting: 'mdi-cloud-sync-outline', disconnected: 'mdi-cloud-off-outline', failed: 'mdi-cloud-alert-outline' })[collab.connection.value])
  const statusText = computed(() => t(`collab.status.${collab.connection.value}`))
</script>

<style scoped>
.editor-surface {
  border: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-radius: 4px;
  padding: 16px;
  min-height: 200px;
}
.editor-toolbar { display: flex; gap: 2px; }
.editor-surface :deep(.ProseMirror) { outline: none; min-height: 168px; }
.editor-surface :deep(.collaboration-carets__caret) {
  position: relative; margin-left: -1px; margin-right: -1px; border-left: 1px solid; border-right: 1px solid;
  word-break: normal; pointer-events: none;
}
.editor-surface :deep(.collaboration-carets__label) {
  position: absolute; top: -1.4em; left: -1px; font-size: 12px; font-weight: 600; line-height: normal;
  padding: 0.1rem 0.3rem; border-radius: 3px 3px 3px 0; color: #fff; white-space: nowrap; user-select: none;
}
</style>
