<template>
  <v-dialog
    v-model="open"
    :aria-label="t('search.title')"
    max-width="640"
    scrollable
    @after-leave="reset"
  >
    <v-card :aria-label="t('search.title')" data-testid="search-dialog">
      <v-card-text class="pb-0">
        <v-text-field
          ref="input"
          v-model="query"
          :aria-activedescendant="activeIndex >= 0 ? `search-hit-${activeIndex}` : undefined"
          aria-controls="search-results"
          aria-expanded="true"
          autofocus
          clearable
          data-testid="search-input"
          hide-details
          :label="t('search.label', { space: spaceName })"
          prepend-inner-icon="mdi-magnify"
          role="combobox"
          variant="outlined"
          @keydown.down.prevent="move(1)"
          @keydown.enter.prevent="openActive"
          @keydown.up.prevent="move(-1)"
        />
      </v-card-text>

      <v-card-text class="pt-2">
        <v-progress-linear v-if="loading" indeterminate />
        <v-alert v-if="error" density="compact" type="error" variant="tonal">{{ error }}</v-alert>

        <div v-else-if="!loading && searched && results.length === 0" aria-live="polite" class="text-body-2 text-medium-emphasis pa-2" data-testid="search-empty">
          {{ t('search.noResults') }}
        </div>

        <div v-else-if="!searched && !loading" class="text-body-2 text-medium-emphasis pa-2">{{ t('search.hint') }}</div>

        <v-list
          v-if="results.length > 0"
          id="search-results"
          data-testid="search-results"
          density="compact"
          role="listbox"
        >
          <!-- Titles are rendered as text (never v-html): the api returns no markup. -->
          <v-list-item
            v-for="(hit, index) in results"
            :id="`search-hit-${index}`"
            :key="hit.id"
            :active="index === activeIndex"
            :aria-selected="index === activeIndex"
            :data-testid="`search-hit-${index}`"
            prepend-icon="mdi-file-document-outline"
            role="option"
            :title="hit.title || t('search.untitled')"
            @click="go(hit.id)"
            @mouseenter="activeIndex = index"
          />
        </v-list>

        <div v-if="results.length >= 25" class="text-caption text-medium-emphasis px-2">{{ t('search.maxHits') }}</div>
      </v-card-text>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import { onBeforeUnmount, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import { searchSpace } from '@/api/generated'
  import { RateLimitedError, rateLimitMessage, retryAfterSeconds } from '@/utils/rateLimit'
  import { moveActive, normalizeQuery, SEARCH_DEBOUNCE_MS } from '@/utils/searchQuery'

  const props = defineProps<{ spaceId: string, spaceName: string }>()
  const open = defineModel<boolean>({ required: true })

  const { t } = useI18n()
  const router = useRouter()

  type Hit = { id: string, title: string }
  const query = ref('')
  const results = ref<Hit[]>([])
  const activeIndex = ref(-1)
  const loading = ref(false)
  const searched = ref(false)
  const error = ref('')

  let timer: ReturnType<typeof setTimeout> | undefined
  // Drops responses from superseded queries (and from a closed dialog).
  let generation = 0

  async function runSearch (q: string, current: number) {
    try {
      const { data, error: apiError, response } = await searchSpace({ path: { spaceId: props.spaceId }, query: { q } })
      if (current !== generation) {
        return
      }
      if (response?.status === 429) {
        throw new RateLimitedError(retryAfterSeconds(response))
      }
      if (apiError || !data) {
        throw new Error(t('search.failed'))
      }
      results.value = data
      activeIndex.value = data.length > 0 ? 0 : -1
      searched.value = true
    } catch (error_) {
      if (current !== generation) {
        return
      }
      results.value = []
      activeIndex.value = -1
      error.value = error_ instanceof RateLimitedError ? rateLimitMessage(t, error_.retryAfter) : t('search.failed')
    } finally {
      if (current === generation) {
        loading.value = false
      }
    }
  }

  watch(query, raw => {
    clearTimeout(timer)
    const current = ++generation
    error.value = ''
    const q = normalizeQuery(raw ?? '')
    if (q === null) {
      results.value = []
      activeIndex.value = -1
      searched.value = false
      loading.value = false
      return
    }
    // The old list belongs to the old query: don't let Enter open from it.
    results.value = []
    activeIndex.value = -1
    loading.value = true
    timer = setTimeout(() => {
      runSearch(q, current)
    }, SEARCH_DEBOUNCE_MS)
  })

  function move (delta: 1 | -1) {
    activeIndex.value = moveActive(activeIndex.value, delta, results.value.length)
    document.querySelector(`#search-hit-${activeIndex.value}`)?.scrollIntoView?.({ block: 'nearest' })
  }

  function openActive () {
    const hit = results.value[activeIndex.value]
    if (hit) {
      go(hit.id)
    }
  }

  function go (pageId: string) {
    open.value = false
    router.push(`/spaces/${props.spaceId}/pages/${pageId}`)
  }

  function reset () {
    clearTimeout(timer)
    generation++
    query.value = ''
    results.value = []
    activeIndex.value = -1
    searched.value = false
    loading.value = false
    error.value = ''
  }

  // A space switch while open would show another space's hits.
  watch(() => props.spaceId, reset)
  onBeforeUnmount(() => {
    clearTimeout(timer)
    generation++
  })
</script>
