<template>
  <v-container v-if="authStore.isOrgAdmin" fluid style="max-width: 760px">
    <h1 class="text-h5 mb-4">{{ t('adminSettings.title') }}</h1>

    <v-alert v-if="loadError" class="mb-4" density="compact" type="error">{{ loadError }}</v-alert>
    <v-progress-linear v-if="loading" indeterminate />

    <template v-if="settings">
      <v-alert
        v-if="message"
        class="mb-4"
        closable
        density="compact"
        :type="messageType"
        @click:close="message = ''"
      >{{ message }}</v-alert>

      <v-card class="mb-6" data-testid="attachment-policy" :title="t('adminSettings.attachmentPolicy')">
        <v-card-text>
          <v-chip class="mb-4" :color="settings.attachment_policy_overridden ? 'warning' : undefined" size="small" variant="tonal">
            {{ settings.attachment_policy_overridden ? t('adminSettings.overridden') : t('adminSettings.usingDefault') }}
          </v-chip>

          <v-textarea
            v-model="allowlistText"
            auto-grow
            :error-messages="allowlistError"
            :hint="t('adminSettings.allowlistHint')"
            :label="t('adminSettings.allowlist')"
            persistent-hint
            rows="4"
          />

          <v-text-field
            v-model="maxMib"
            class="mt-2"
            :error-messages="sizeError"
            :hint="t('adminSettings.maxSizeHint')"
            :label="t('adminSettings.maxSize')"
            persistent-hint
            type="number"
          />

          <div class="text-caption mt-3">
            {{ t('adminSettings.defaultLabel') }}: {{ bytesToMib(settings.attachment_default_max_size_bytes) }} MiB,
            {{ settings.attachment_default_allowlist.join(', ') }}
          </div>
        </v-card-text>

        <v-card-actions>
          <v-btn data-testid="reset-policy" :disabled="!settings.attachment_policy_overridden" :loading="saving" @click="resetPolicy">{{ t('adminSettings.reset') }}</v-btn>
          <v-spacer />

          <v-btn
            color="primary"
            data-testid="save-policy"
            :loading="saving"
            variant="flat"
            @click="savePolicy"
          >{{ t('adminSettings.save') }}</v-btn>
        </v-card-actions>
      </v-card>

      <v-card :title="t('adminSettings.internalLogin')">
        <v-card-text>
          <v-switch
            v-model="internalLoginDisabled"
            color="primary"
            data-testid="internal-login-switch"
            :disabled="saving"
            hide-details="auto"
            :hint="t('adminSettings.internalLoginHelp')"
            :label="t('adminSettings.internalLoginDisabled')"
            persistent-hint
            @update:model-value="onToggleInternalLogin"
          />
        </v-card-text>
      </v-card>
    </template>

    <ConfirmDialog
      v-model="confirmLoginOpen"
      :body="t('adminSettings.internalLoginHelp')"
      :confirm-label="t('adminSettings.internalLoginDisabled')"
      :loading="saving"
      :title="t('adminSettings.internalLoginDisabled')"
      @confirm="confirmDisable"
      @update:model-value="onConfirmModel"
    />
  </v-container>

  <v-container v-else fluid><v-alert type="warning">{{ t('common.noAccess') }}</v-alert></v-container>
</template>

<script lang="ts" setup>
  import type { AdminSettings } from '@/api/generated'
  import { onMounted, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { getAdminSettings, updateAdminSettings } from '@/api/generated'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import { useAuthStore } from '@/stores/auth'
  import { bytesToMib, formatAllowlist, MAX_ATTACHMENT_MIB, mibToBytes, parseAllowlist } from '@/utils/accounts'

  const { t } = useI18n()
  const authStore = useAuthStore()

  const settings = ref<AdminSettings | null>(null)
  const loading = ref(false)
  const loadError = ref('')
  const saving = ref(false)
  const message = ref('')
  const messageType = ref<'success' | 'error'>('success')

  const allowlistText = ref('')
  const maxMib = ref('')
  const internalLoginDisabled = ref(false)
  const allowlistError = ref('')
  const sizeError = ref('')

  function apply (s: AdminSettings) {
    settings.value = s
    allowlistText.value = formatAllowlist(s.attachment_allowlist)
    maxMib.value = String(bytesToMib(s.attachment_max_size_bytes))
    internalLoginDisabled.value = s.internal_login_disabled
  }

  async function patch (body: Parameters<typeof updateAdminSettings>[0]['body']) {
    saving.value = true
    message.value = ''
    try {
      const { data, error } = await updateAdminSettings({ body })
      if (error || !data) {
        throw new Error(error?.error ?? t('adminSettings.saveFailed'))
      }
      apply(data)
      message.value = t('adminSettings.saved')
      messageType.value = 'success'
    } catch (error) {
      message.value = error instanceof Error ? error.message : t('adminSettings.saveFailed')
      messageType.value = 'error'
      // Re-sync the switch with the server value after a failed toggle.
      if (settings.value) {
        internalLoginDisabled.value = settings.value.internal_login_disabled
      }
    } finally {
      saving.value = false
    }
  }

  function savePolicy () {
    allowlistError.value = sizeError.value = ''
    const list = parseAllowlist(allowlistText.value)
    const mib = Number(maxMib.value)
    if (list.length === 0) {
      allowlistError.value = t('adminSettings.allowlistRequired')
    }
    if (!Number.isFinite(mib) || mib < 0.01 || mib > MAX_ATTACHMENT_MIB) {
      sizeError.value = t('adminSettings.sizeInvalid')
    }
    if (allowlistError.value || sizeError.value) {
      return
    }
    return patch({ attachmentAllowlist: list, attachmentMaxSizeBytes: mibToBytes(mib) })
  }

  // null resets a field to the deployment default (Decision 45).
  function resetPolicy () {
    return patch({ attachmentAllowlist: null, attachmentMaxSizeBytes: null })
  }

  const confirmLoginOpen = ref(false)
  // Disabling internal login is instance-wide, so ask first; re-enabling is safe.
  function onToggleInternalLogin (value: boolean | null) {
    if (value === true) {
      confirmLoginOpen.value = true
      return
    }
    return patch({ internalLoginDisabled: false })
  }

  function onConfirmModel (open: boolean) {
    if (!open && settings.value) {
      internalLoginDisabled.value = settings.value.internal_login_disabled
    }
  }

  async function confirmDisable () {
    await patch({ internalLoginDisabled: true })
    confirmLoginOpen.value = false
  }

  onMounted(async () => {
    if (!authStore.isOrgAdmin) {
      return
    }
    loading.value = true
    try {
      const { data, error } = await getAdminSettings()
      if (error || !data) {
        throw new Error(error?.error ?? t('common.loadFailed'))
      }
      apply(data)
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : t('common.loadFailed')
    } finally {
      loading.value = false
    }
  })
</script>
