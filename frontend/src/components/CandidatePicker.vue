<template>
  <v-autocomplete
    v-model="selected"
    v-model:search="search"
    clearable
    density="compact"
    hide-details
    :items="items"
    :label="label"
    :loading="loading"
    :no-data-text="t('members.noResults')"
    no-filter
    return-object
  />
</template>

<script lang="ts" setup>
  import type { Candidate } from '@/utils/accounts'
  import { onBeforeUnmount, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'

  const props = defineProps<{ label: string, fetcher: (q: string) => Promise<Candidate[]>, minChars?: number }>()
  const selected = defineModel<Candidate | null>({ default: null })
  const { t } = useI18n()

  const search = ref('')
  const items = ref<Candidate[]>([])
  const loading = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let seq = 0

  watch(search, value => {
    clearTimeout(timer)
    // The autocomplete echoes the selected item's title into `search`; don't re-query for that.
    if (selected.value && value === selected.value.title) {
      return
    }
    if ((value ?? '').trim().length < (props.minChars ?? 1)) {
      items.value = selected.value ? [selected.value] : []
      return
    }
    timer = setTimeout(async () => {
      const mine = ++seq
      loading.value = true
      try {
        const result = await props.fetcher(value.trim())
        if (mine === seq) {
          items.value = result
        }
      } catch {
        if (mine === seq) {
          items.value = []
        }
      } finally {
        if (mine === seq) {
          loading.value = false
        }
      }
    }, 250)
  })

  onBeforeUnmount(() => clearTimeout(timer))
</script>
