<template>
  <v-container fluid>
    <v-alert v-if="loadError" density="compact" type="error">{{ loadError }}</v-alert>
    <v-progress-linear v-else-if="pagesStore.pageLoading && !pagesStore.currentPage" indeterminate />
    <PageEditor v-else-if="pagesStore.currentPage" :key="pagesStore.currentPage.id" :page="pagesStore.currentPage" />
  </v-container>
</template>

<script lang="ts" setup>
  import { ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import PageEditor from '@/components/PageEditor.vue'
  import { usePagesStore } from '@/stores/pages'

  const props = defineProps<{ pageId: string }>()
  const { t } = useI18n()
  const pagesStore = usePagesStore()
  const loadError = ref('')

  watch(() => props.pageId, async id => {
    loadError.value = ''
    try {
      await pagesStore.fetchPage(id)
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : t('editor.loadFailed')
    }
  }, { immediate: true })
</script>
