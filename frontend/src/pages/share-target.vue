<template>
  <v-container class="share-target" max-width="800">
    <h1 class="text-h5 mb-4">{{ t('share.title') }}</h1>

    <v-progress-linear v-if="phase === 'loading'" indeterminate />

    <v-alert v-else-if="phase === 'missing'" type="warning">{{ t('share.missing') }}</v-alert>

    <template v-else-if="share">
      <v-card class="mb-4" variant="outlined">
        <v-card-title class="text-subtitle-1">{{ t('share.files', share.files.length) }}</v-card-title>

        <v-list density="compact">
          <v-list-item v-for="(row, index) in rows" :key="index" :subtitle="rowSubtitle(row)" :title="row.file.name">
            <template #append>
              <v-progress-circular v-if="row.status === 'uploading'" :model-value="row.progress * 100" size="24" />
              <v-icon v-else-if="row.status === 'done'" color="success" icon="mdi-check-circle" />
              <v-icon v-else-if="row.status === 'error'" color="error" icon="mdi-alert-circle" />
            </template>
            <!-- Server message as-is (422 policy rejections). -->
            <template v-if="row.status === 'error'" #subtitle>
              <span class="text-error" role="alert">{{ row.error }}</span>
            </template>
          </v-list-item>
        </v-list>

        <v-card-text v-if="share.text || share.title" class="text-body-2 text-medium-emphasis">
          {{ share.title }} {{ share.text }}
        </v-card-text>
      </v-card>

      <v-card v-if="!started">
        <v-card-title class="text-subtitle-1">{{ t('share.pickDestination') }}</v-card-title>

        <v-card-text>
          <v-alert v-if="editableSpaces.length === 0 && !spacesStore.loading" density="compact" type="info">{{ t('share.noEditableSpaces') }}</v-alert>

          <template v-else>
            <v-select
              v-model="spaceId"
              item-title="name"
              item-value="id"
              :items="editableSpaces"
              :label="t('share.space')"
              @update:model-value="loadTree"
            />

            <template v-if="spaceId">
              <v-text-field
                v-model="filter"
                clearable
                density="compact"
                :label="t('share.search')"
                prepend-inner-icon="mdi-magnify"
              />

              <v-alert v-if="treeError" density="compact" type="error">{{ treeError }}</v-alert>

              <v-list
                v-model:selected="selected"
                density="compact"
                max-height="320"
                select-strategy="single-leaf"
                style="overflow-y: auto"
              >
                <v-list-item
                  v-for="item in visiblePages"
                  :key="item.id"
                  :style="{ paddingLeft: `${16 + item.depth * 16}px` }"
                  :title="item.title"
                  :value="item.id"
                />

                <v-list-item v-if="visiblePages.length === 0" disabled :title="t('share.noPages')" />
              </v-list>

              <v-btn class="mt-2" :loading="creating" variant="text" @click="createAndSelect">
                <v-icon icon="mdi-plus" start />{{ t('share.newPage') }}
              </v-btn>

              <v-alert v-if="createError" class="mt-2" density="compact" type="error">{{ createError }}</v-alert>
            </template>
          </template>
        </v-card-text>

        <v-card-actions>
          <v-btn variant="text" @click="cancel">{{ t('share.cancel') }}</v-btn>
          <v-spacer />
          <v-btn color="primary" :disabled="selected.length === 0" @click="start">{{ t('share.attach') }}</v-btn>
        </v-card-actions>
      </v-card>

      <div v-else class="d-flex ga-2">
        <v-btn v-if="finished && failedCount > 0" variant="outlined" @click="retryFailed">{{ t('share.retry') }}</v-btn>
        <v-spacer />
        <v-btn v-if="finished" color="primary" @click="openPage">{{ t('share.openPage') }}</v-btn>
      </div>
    </template>
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRoute, useRouter } from 'vue-router'
  import { uploadAttachment } from '@/api/attachments'
  import { usePagesStore } from '@/stores/pages'
  import { useSpacesStore } from '@/stores/spaces'
  import { formatSize } from '@/utils/attachments'
  import { flattenPageTree } from '@/utils/pageTree'
  import { roleAtLeast } from '@/utils/permissions'
  import { clearShare, isValidShareKey, readShare, type SharedPayload } from '@/utils/shareStash'

  interface Row {
    file: File
    status: 'pending' | 'uploading' | 'done' | 'error'
    progress: number
    error?: string
  }

  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const spacesStore = useSpacesStore()
  const pagesStore = usePagesStore()

  const phase = ref<'loading' | 'ready' | 'missing'>('loading')
  const share = ref<SharedPayload | null>(null)
  const rows = ref<Row[]>([])
  const spaceId = ref<string | null>(null)
  const selected = ref<string[]>([])
  const filter = ref('')
  const treeError = ref('')
  const createError = ref('')
  const creating = ref(false)
  const started = ref(false)
  const finished = ref(false)

  const key = computed(() => typeof route.query.key === 'string' ? route.query.key : null)

  // Only spaces where the caller is editor+ can take an attachment; locked
  // pages (writes refused for everyone) are filtered out below.
  const editableSpaces = computed(() => spacesStore.spaces.filter(space => roleAtLeast(space.my_role, 'editor')))
  const visiblePages = computed(() => {
    const needle = (filter.value ?? '').trim().toLowerCase()
    return flattenPageTree(pagesStore.treesBySpace[spaceId.value ?? ''] ?? [])
      .filter(item => !item.locked && (!needle || item.title.toLowerCase().includes(needle)))
      .map(item => needle ? { ...item, depth: 0 } : item)
  })
  const failedCount = computed(() => rows.value.filter(row => row.status === 'error').length)

  function rowSubtitle (row: Row) {
    return formatSize(row.file.size)
  }

  onMounted(async () => {
    if (!isValidShareKey(key.value)) {
      phase.value = 'missing'
      return
    }
    try {
      share.value = await readShare(key.value)
    } catch {
      share.value = null
    }
    if (!share.value || share.value.files.length === 0) {
      phase.value = 'missing'
      return
    }
    rows.value = share.value.files.map(file => reactive<Row>({ file, status: 'pending', progress: 0 }))
    phase.value = 'ready'
    if (spacesStore.spaces.length === 0) {
      await spacesStore.fetchSpaces().catch(() => {})
    }
    if (editableSpaces.value.length === 1) {
      spaceId.value = editableSpaces.value[0]!.id
      await loadTree()
    }
  })

  async function loadTree () {
    selected.value = []
    treeError.value = ''
    if (!spaceId.value) return
    try {
      await pagesStore.fetchTree(spaceId.value)
    } catch (error) {
      treeError.value = error instanceof Error ? error.message : t('nav.loadFailed')
    }
  }

  async function createAndSelect () {
    if (!spaceId.value) return
    creating.value = true
    createError.value = ''
    try {
      const page = await pagesStore.createPage(spaceId.value, share.value?.title || share.value?.files[0]?.name || t('space.untitled'), null)
      selected.value = [page.id]
    } catch (error) {
      createError.value = error instanceof Error ? error.message : t('space.createFailed')
    } finally {
      creating.value = false
    }
  }

  // Sequential, one request per file (Decision 32), reusing the same upload
  // function as the in-page attachment panel. A failure doesn't stop the rest.
  async function uploadPending (pageId: string) {
    for (const row of rows.value) {
      if (row.status === 'done') continue
      row.status = 'uploading'
      row.progress = 0
      row.error = undefined
      try {
        await uploadAttachment(pageId, row.file, { onProgress: fraction => {
          row.progress = fraction
        } })
        row.status = 'done'
      } catch (error) {
        row.status = 'error'
        row.error = error instanceof Error ? error.message : t('share.failed')
      }
    }
  }

  async function start () {
    const pageId = selected.value[0]
    if (!pageId) return
    started.value = true
    finished.value = false
    await uploadPending(pageId)
    finished.value = true
    if (failedCount.value === 0) {
      await openPage()
    }
  }

  async function retryFailed () {
    finished.value = false
    await uploadPending(selected.value[0]!)
    finished.value = true
    if (failedCount.value === 0) {
      await openPage()
    }
  }

  async function discardStash () {
    if (key.value) await clearShare(key.value).catch(() => {})
  }

  async function openPage () {
    await discardStash()
    router.push(`/spaces/${spaceId.value}/pages/${selected.value[0]}`)
  }

  async function cancel () {
    await discardStash()
    router.push('/')
  }
</script>
