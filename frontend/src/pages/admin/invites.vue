<template>
  <v-container v-if="authStore.isOrgAdmin" fluid>
    <div class="d-flex align-center mb-4">
      <h1 class="text-h5">{{ t('invites.title') }}</h1>
      <v-spacer />
      <v-btn color="primary" data-testid="create-invite" prepend-icon="mdi-plus" @click="openCreate">{{ t('invites.create') }}</v-btn>
    </div>

    <v-alert v-if="loadError" class="mb-4" density="compact" type="error">{{ loadError }}</v-alert>
    <v-progress-linear v-if="loading" indeterminate />

    <v-table v-if="invites.length > 0" data-testid="invite-table">
      <thead>
        <tr>
          <th>{{ t('invites.colEmail') }}</th>
          <th>{{ t('invites.colStatus') }}</th>
          <th>{{ t('invites.colExpires') }}</th>
          <th>{{ t('invites.colCreatedBy') }}</th>
          <th />
        </tr>
      </thead>

      <tbody>
        <tr v-for="invite in invites" :key="invite.id">
          <td>
            {{ invite.email ?? t('invites.anyEmail') }}
            <v-chip v-if="invite.grants_org_admin" class="ml-1" color="primary" size="x-small">{{ t('invites.admin') }}</v-chip>
          </td>

          <td>{{ t(`invites.status.${invite.status}`) }}</td>
          <td>{{ invite.expires_at ? new Date(invite.expires_at).toLocaleString() : '—' }}</td>
          <td>{{ invite.created_by_display_name ?? '—' }}</td>

          <td class="text-right">
            <v-btn
              v-if="invite.status === 'pending'"
              :data-testid="`revoke-${invite.id}`"
              size="small"
              variant="text"
              @click="toRevoke = invite"
            >{{ t('invites.revoke') }}</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>

    <p v-else-if="!loading && !loadError">{{ t('invites.emptyList') }}</p>

    <v-dialog v-model="createOpen" max-width="480" persistent>
      <v-card :title="created ? t('invites.createdTitle') : t('invites.create')">
        <v-card-text v-if="created">
          <v-alert class="mb-3" density="compact" type="warning">{{ t('invites.createdWarning') }}</v-alert>

          <v-text-field
            data-testid="invite-link"
            hide-details
            :label="t('invites.link')"
            :model-value="createdLink"
            readonly
          >
            <template #append-inner>
              <v-btn data-testid="copy-invite" size="small" variant="text" @click="copy">{{ copied ? t('common.copied') : t('common.copy') }}</v-btn>
            </template>
          </v-text-field>
        </v-card-text>

        <v-form v-else @submit.prevent="submit">
          <v-card-text>
            <v-text-field v-model="email" autofocus :label="t('invites.email')" type="email" />

            <v-text-field
              v-model.number="days"
              :hint="t('invites.expiresHint')"
              :label="t('invites.expiresInDays')"
              max="90"
              min="1"
              persistent-hint
              type="number"
            />

            <v-checkbox v-model="grantsOrgAdmin" density="compact" :label="t('invites.grantsOrgAdmin')" />
            <v-alert v-if="createError" density="compact" type="error">{{ createError }}</v-alert>
          </v-card-text>

          <v-card-actions>
            <v-spacer />
            <v-btn @click="createOpen = false">{{ t('common.cancel') }}</v-btn>

            <v-btn
              color="primary"
              data-testid="submit-invite"
              :loading="saving"
              type="submit"
              variant="flat"
            >{{ t('invites.create') }}</v-btn>
          </v-card-actions>
        </v-form>

        <v-card-actions v-if="created">
          <v-spacer />
          <v-btn data-testid="close-invite" variant="flat" @click="createOpen = false">{{ t('common.close') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <ConfirmDialog
      :body="t('invites.revokeConfirm')"
      :confirm-label="t('invites.revoke')"
      :error="revokeError"
      :loading="saving"
      :model-value="toRevoke !== null"
      :title="t('invites.revoke')"
      @confirm="revoke"
      @update:model-value="toRevoke = null"
    />
  </v-container>

  <v-container v-else fluid><v-alert type="warning">{{ t('common.noAccess') }}</v-alert></v-container>
</template>

<script lang="ts" setup>
  import type { Invite } from '@/api/generated'
  import { computed, onMounted, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { createInvite, listInvites, revokeInvite } from '@/api/generated'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import { useAuthStore } from '@/stores/auth'
  import { expiryFromDays, inviteLink } from '@/utils/accounts'

  const { t } = useI18n()
  const authStore = useAuthStore()

  const invites = ref<Invite[]>([])
  const loading = ref(false)
  const loadError = ref('')
  const saving = ref(false)

  const createOpen = ref(false)
  const email = ref('')
  const days = ref<number | null>(7)
  const grantsOrgAdmin = ref(false)
  const createError = ref('')
  // The one-time secret: lives only in this ref, cleared when the dialog closes.
  const created = ref<string | null>(null)
  const createdLink = computed(() => created.value ? inviteLink(globalThis.location.origin, created.value) : '')
  const copied = ref(false)

  const toRevoke = ref<Invite | null>(null)
  const revokeError = ref('')

  watch(toRevoke, () => {
    revokeError.value = ''
  })

  watch(createOpen, open => {
    if (!open) {
      created.value = null
      copied.value = false
    }
  })

  async function load () {
    loading.value = true
    loadError.value = ''
    try {
      const { data, error } = await listInvites()
      if (error) {
        throw new Error(error.error)
      }
      invites.value = data ?? []
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : t('common.loadFailed')
    } finally {
      loading.value = false
    }
  }

  function openCreate () {
    email.value = createError.value = ''
    days.value = 7
    grantsOrgAdmin.value = false
    created.value = null
    createOpen.value = true
  }

  async function submit () {
    saving.value = true
    createError.value = ''
    try {
      const { data, error } = await createInvite({
        body: {
          ...(email.value.trim() ? { email: email.value.trim() } : {}),
          grantsOrgAdmin: grantsOrgAdmin.value,
          expiresAt: expiryFromDays(days.value),
        },
      })
      if (error || !data) {
        throw new Error(error?.error ?? t('invites.failed'))
      }
      created.value = data.token
      await load()
    } catch (error) {
      createError.value = error instanceof Error ? error.message : t('invites.failed')
    } finally {
      saving.value = false
    }
  }

  async function copy () {
    try {
      await navigator.clipboard.writeText(createdLink.value)
      copied.value = true
    } catch {
      // Clipboard may be blocked (insecure origin); the field is selectable.
      copied.value = false
    }
  }

  async function revoke () {
    if (!toRevoke.value) {
      return
    }
    saving.value = true
    revokeError.value = ''
    try {
      const { error } = await revokeInvite({ path: { inviteId: toRevoke.value.id } })
      if (error) {
        throw new Error(error.error)
      }
      toRevoke.value = null
      await load()
    } catch (error) {
      revokeError.value = error instanceof Error ? error.message : t('invites.revokeFailed')
    } finally {
      saving.value = false
    }
  }

  onMounted(() => {
    if (authStore.isOrgAdmin) {
      load()
    }
  })
</script>
