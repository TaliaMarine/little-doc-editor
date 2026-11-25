<template>
  <v-dialog v-model="open" max-width="560">
    <v-card :title="t('newPage.title')">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-alert v-if="templatesError" class="mb-3" density="compact" type="warning">{{ t('newPage.templatesFailed') }}</v-alert>

          <v-text-field
            v-model="title"
            autofocus
            :hint="selected ? t('newPage.titleHint') : undefined"
            :label="t('newPage.pageTitle')"
            persistent-hint
          />

          <div v-if="templates.length > 0" class="text-subtitle-2 mt-4 mb-1">{{ t('newPage.pickTemplate') }}</div>

          <v-list
            v-if="templates.length > 0"
            v-model:selected="selectedKeys"
            data-testid="template-list"
            density="compact"
            mandatory
            select-strategy="single-independent"
          >
            <v-list-item
              v-for="tpl in templates"
              :key="tpl.key"
              :data-testid="`template-${tpl.key}`"
              :subtitle="templateText(tpl, 'description')"
              :title="templateText(tpl, 'name')"
              :value="tpl.key"
            />
          </v-list>

          <v-alert v-if="error" class="mt-2" density="compact" type="error">{{ error }}</v-alert>
        </v-card-text>

        <v-card-actions>
          <v-spacer />
          <v-btn @click="open = false">{{ t('common.cancel') }}</v-btn>

          <v-btn
            color="primary"
            data-testid="create-page"
            :disabled="!canSubmit"
            :loading="saving"
            type="submit"
            variant="flat"
          >{{ t('newPage.create') }}</v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import type { PageTemplate } from '@/api/generated'
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import { listPageTemplates } from '@/api/generated'
  import { usePagesStore } from '@/stores/pages'

  const props = defineProps<{ spaceId: string, parentId?: string | null }>()
  const open = defineModel<boolean>({ required: true })
  const { t, te } = useI18n()
  const router = useRouter()
  const pagesStore = usePagesStore()

  const templates = ref<PageTemplate[]>([])
  const templatesError = ref(false)
  const selectedKeys = ref<string[]>(['blank'])
  const title = ref('')
  const saving = ref(false)
  const error = ref('')

  const selected = computed(() => selectedKeys.value[0] ?? 'blank')
  // Blank has no server content to default the title from beyond "Untitled";
  // the server defaults the title for every template when none is given.
  const canSubmit = computed(() => templatesError.value ? title.value.trim() !== '' : true)

  function templateText (tpl: PageTemplate, field: 'name' | 'description') {
    const key = `newPage.templates.${tpl.key}.${field}`
    return te(key) ? t(key) : tpl[field]
  }

  let loaded = false
  watch(open, async isOpen => {
    if (!isOpen) {
      return
    }
    title.value = error.value = ''
    selectedKeys.value = ['blank']
    if (loaded) {
      return
    }
    try {
      const { data, error: apiError } = await listPageTemplates()
      if (apiError || !data) {
        throw new Error('templates')
      }
      templates.value = data
      templatesError.value = false
      loaded = true
    } catch {
      templatesError.value = true
    }
  }, { immediate: true })

  async function submit () {
    saving.value = true
    error.value = ''
    try {
      // The server fills in the default title when a template is chosen and no
      // title given; a blank page via the legacy path needs an explicit one.
      const key = templatesError.value ? undefined : selected.value
      const page = await pagesStore.createPage(props.spaceId, title.value.trim(), props.parentId ?? null, key)
      open.value = false
      router.push(`/spaces/${props.spaceId}/pages/${page!.id}`)
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('newPage.failed')
    } finally {
      saving.value = false
    }
  }
</script>
