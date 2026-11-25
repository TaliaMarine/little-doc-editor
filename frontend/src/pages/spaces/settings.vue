<template>
  <v-container v-if="canManage" fluid style="max-width: 720px">
    <h1 class="text-h5 mb-4">{{ t('spaceSettings.title') }}</h1>

    <v-alert
      v-if="message"
      class="mb-4"
      closable
      density="compact"
      :type="messageType"
      @click:close="message = ''"
    >{{ message }}</v-alert>

    <v-form @submit.prevent="save">
      <v-card class="mb-6" :title="t('spaceSettings.general')">
        <v-card-text>
          <v-text-field v-model="name" data-testid="space-name" :error-messages="nameError" :label="t('spaceSettings.name')" />
          <v-text-field v-model="icon" data-testid="space-icon" :label="t('spaceSettings.icon')" />
        </v-card-text>
      </v-card>

      <v-card class="mb-6" :title="t('spaceSettings.history')">
        <v-card-text>
          <p class="text-body-2 mb-3">{{ t('spaceSettings.historyHint') }}</p>

          <v-text-field
            v-model="historyMaxCount"
            data-testid="history-count"
            :error-messages="countError"
            :label="t('spaceSettings.historyMaxCount')"
            type="number"
          />

          <v-text-field
            v-model="historyMaxAgeDays"
            data-testid="history-age"
            :error-messages="ageError"
            :label="t('spaceSettings.historyMaxAgeDays')"
            type="number"
          />
        </v-card-text>
      </v-card>

      <v-btn
        color="primary"
        data-testid="save-space"
        :loading="saving"
        type="submit"
        variant="flat"
      >{{ t('spaceSettings.save') }}</v-btn>
    </v-form>

    <v-card v-if="isOwner" class="mt-10" color="error" variant="outlined">
      <v-card-title>{{ t('spaceSettings.danger') }}</v-card-title>

      <v-card-actions>
        <v-btn color="error" data-testid="delete-space" variant="flat" @click="deleteOpen = true">{{ t('spaceSettings.delete') }}</v-btn>
      </v-card-actions>
    </v-card>

    <ConfirmDialog
      v-model="deleteOpen"
      :body="t('spaceSettings.deleteBody', { name: space?.name ?? '' })"
      :confirm-label="t('spaceSettings.delete')"
      :error="deleteError"
      :loading="deleting"
      :require-text="space?.name"
      :require-text-label="t('spaceSettings.deleteTypeName')"
      :title="t('spaceSettings.deleteTitle')"
      @confirm="remove"
    />
  </v-container>

  <v-container v-else fluid>
    <v-progress-linear v-if="spacesStore.loading || !space" indeterminate />
    <v-alert v-else type="warning">{{ t('members.needAdmin') }}</v-alert>
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import { useSpacesStore } from '@/stores/spaces'
  import { parseOptionalPositiveInt } from '@/utils/accounts'
  import { roleAtLeast } from '@/utils/permissions'

  const props = defineProps<{ spaceId: string }>()
  const { t } = useI18n()
  const router = useRouter()
  const spacesStore = useSpacesStore()

  const space = computed(() => spacesStore.spaces.find(s => s.id === props.spaceId))
  // Rename/icon/history: owner/admin. Delete: owner only (org admins report owner) — Decision 46.
  const canManage = computed(() => roleAtLeast(space.value?.my_role, 'admin'))
  const isOwner = computed(() => space.value?.my_role === 'owner')

  const name = ref('')
  const icon = ref('')
  const historyMaxCount = ref('')
  const historyMaxAgeDays = ref('')
  const nameError = ref('')
  const countError = ref('')
  const ageError = ref('')
  const saving = ref(false)
  const message = ref('')
  const messageType = ref<'success' | 'error'>('success')

  const deleteOpen = ref(false)
  const deleting = ref(false)
  const deleteError = ref('')

  watch(space, s => {
    if (s) {
      name.value = s.name
      icon.value = s.icon ?? ''
      historyMaxCount.value = s.history_max_count?.toString() ?? ''
      historyMaxAgeDays.value = s.history_max_age_days?.toString() ?? ''
    }
  }, { immediate: true })

  async function save () {
    nameError.value = countError.value = ageError.value = message.value = ''
    const count = parseOptionalPositiveInt(historyMaxCount.value)
    const age = parseOptionalPositiveInt(historyMaxAgeDays.value)
    if (!name.value.trim()) {
      nameError.value = t('spaceSettings.nameRequired')
    }
    if (count === undefined) {
      countError.value = t('spaceSettings.positiveInt')
    }
    if (age === undefined) {
      ageError.value = t('spaceSettings.positiveInt')
    }
    if (nameError.value || countError.value || ageError.value) {
      return
    }
    saving.value = true
    try {
      await spacesStore.updateSpace(props.spaceId, {
        name: name.value.trim(),
        icon: icon.value.trim() || null,
        historyMaxCount: count,
        historyMaxAgeDays: age,
      })
      message.value = t('spaceSettings.saved')
      messageType.value = 'success'
    } catch (error) {
      message.value = error instanceof Error ? error.message : t('spaceSettings.saveFailed')
      messageType.value = 'error'
    } finally {
      saving.value = false
    }
  }

  async function remove () {
    deleting.value = true
    deleteError.value = ''
    try {
      await spacesStore.deleteSpace(props.spaceId)
      deleteOpen.value = false
      router.push('/')
    } catch (error) {
      deleteError.value = error instanceof Error ? error.message : t('spaceSettings.deleteFailed')
    } finally {
      deleting.value = false
    }
  }
</script>
