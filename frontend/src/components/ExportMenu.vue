<template>
  <div class="d-inline-block">
    <v-menu>
      <template #activator="{ props: menuProps }">
        <v-btn
          v-bind="menuProps"
          data-testid="export-menu"
          :loading="working"
          prepend-icon="mdi-download"
          variant="outlined"
        >{{ t('exportMenu.label') }}</v-btn>
      </template>

      <v-list density="compact">
        <template v-for="(scope, i) in scopes" :key="scope.key">
          <v-divider v-if="i > 0" />
          <v-list-subheader>{{ t(`exportMenu.${scope.key}`) }}</v-list-subheader>

          <v-list-item
            v-for="format in FORMATS"
            :key="`${scope.key}-${format}`"
            :data-testid="`export-${scope.key}-${format}`"
            :title="t(`exportMenu.${format}`)"
            @click="run(scope.target, format)"
          />
        </template>
      </v-list>
    </v-menu>

    <v-snackbar v-model="showError" color="error" :timeout="6000">{{ errorText }}</v-snackbar>
  </div>
</template>

<script lang="ts" setup>
  import type { ExportFormat } from '@/api/generated'
  import { computed, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { ExportError, type ExportTarget, fetchExport, printHtmlBlob, saveBlob } from '@/utils/download'

  const props = defineProps<{ pageId?: string, spaceId?: string }>()
  const { t } = useI18n()
  const FORMATS: ExportFormat[] = ['md', 'html', 'pdf']

  const working = ref(false)
  const showError = ref(false)
  const errorText = ref('')

  const scopes = computed(() => {
    const list: { key: string, target: ExportTarget }[] = []
    if (props.pageId) {
      list.push({ key: 'page', target: { kind: 'page', id: props.pageId } }, { key: 'descendants', target: { kind: 'page', id: props.pageId, descendants: true } })
    }
    if (props.spaceId) {
      list.push({ key: 'space', target: { kind: 'space', id: props.spaceId } })
    }
    return list
  })

  async function run (target: ExportTarget, format: ExportFormat) {
    working.value = true
    try {
      const { blob, filename } = await fetchExport(target, format)
      await (format === 'pdf' ? printHtmlBlob(blob) : saveBlob(blob, filename))
    } catch (error) {
      errorText.value = error instanceof ExportError && error.status === 413
        ? t('exportMenu.tooMany')
        : (error instanceof Error && error.message && error.message !== 'failed' ? error.message : t('exportMenu.failed'))
      showError.value = true
    } finally {
      working.value = false
    }
  }
</script>
