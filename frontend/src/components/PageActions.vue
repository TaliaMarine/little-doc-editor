<template>
  <div class="d-flex align-center flex-wrap ga-2 mb-2" data-testid="page-actions">
    <ExportMenu v-if="page.my_role" :page-id="page.id" :space-id="page.space_id" />

    <v-btn
      v-if="canEdit"
      data-testid="new-subpage"
      prepend-icon="mdi-file-plus-outline"
      variant="outlined"
      @click="newPageOpen = true"
    >{{ t('pageActions.newChild') }}</v-btn>

    <v-btn
      v-if="canEdit"
      data-testid="toggle-lock"
      :loading="busy"
      :prepend-icon="page.locked ? 'mdi-lock-open-variant-outline' : 'mdi-lock-outline'"
      variant="outlined"
      @click="toggleLock"
    >{{ page.locked ? t('pageActions.unlock') : t('pageActions.lock') }}</v-btn>

    <v-btn
      v-if="canEdit"
      color="error"
      data-testid="delete-page"
      prepend-icon="mdi-delete-outline"
      variant="outlined"
      @click="confirmOpen = true"
    >{{ t('pageActions.delete') }}</v-btn>

    <v-alert
      v-if="error"
      closable
      density="compact"
      type="error"
      @click:close="error = ''"
    >{{ error }}</v-alert>

    <NewPageDialog v-if="canEdit" v-model="newPageOpen" :parent-id="page.id" :space-id="page.space_id" />

    <ConfirmDialog
      v-if="canEdit"
      v-model="confirmOpen"
      :body="t('pageActions.deleteBody', { title: page.title })"
      :confirm-label="t('pageActions.delete')"
      :error="deleteError"
      :loading="busy"
      :title="t('pageActions.deleteTitle')"
      @confirm="remove"
    />
  </div>
</template>

<script lang="ts" setup>
  import type { Page } from '@/api/generated'
  import { computed, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import ExportMenu from '@/components/ExportMenu.vue'
  import NewPageDialog from '@/components/NewPageDialog.vue'
  import { usePagesStore } from '@/stores/pages'
  import { canManagePage } from '@/utils/permissions'

  const props = defineProps<{ page: Pick<Page, 'id' | 'space_id' | 'title' | 'locked' | 'my_role'> }>()
  const { t } = useI18n()
  const router = useRouter()
  const pagesStore = usePagesStore()

  // Delete / lock / sub-page are member-only and need editor+ (Decision 46);
  // an edit-link guest has my_role null, so everything here is hidden for them.
  const canEdit = computed(() => canManagePage(props.page))
  const busy = ref(false)
  const newPageOpen = ref(false)
  const confirmOpen = ref(false)
  const error = ref('')
  const deleteError = ref('')

  async function toggleLock () {
    busy.value = true
    error.value = ''
    try {
      await pagesStore.setLocked(props.page.id, !props.page.locked)
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('pageActions.failed')
    } finally {
      busy.value = false
    }
  }

  async function remove () {
    busy.value = true
    deleteError.value = ''
    try {
      await pagesStore.deletePage(props.page.space_id, props.page.id)
      confirmOpen.value = false
      router.push(`/spaces/${props.page.space_id}`)
    } catch (error_) {
      deleteError.value = error_ instanceof Error ? error_.message : t('pageActions.failed')
    } finally {
      busy.value = false
    }
  }
</script>
