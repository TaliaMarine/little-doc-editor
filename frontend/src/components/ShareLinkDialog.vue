<template>
  <v-dialog v-model="open" max-width="720" scrollable>
    <v-card data-testid="share-dialog" :title="t('shareLink.title')">
      <v-card-text>
        <p class="text-body-2 mb-4">{{ t('shareLink.intro') }}</p>

        <v-form class="mb-4" data-testid="share-create" @submit.prevent="create">
          <v-btn-toggle
            v-model="permission"
            class="mb-3"
            color="primary"
            data-testid="share-permission"
            mandatory
            variant="outlined"
          >
            <v-btn data-testid="perm-view" value="view">{{ t('shareLink.view') }}</v-btn>
            <v-btn data-testid="perm-edit" value="edit">{{ t('shareLink.edit') }}</v-btn>
          </v-btn-toggle>

          <v-text-field
            v-model="expiry"
            :error-messages="expiryError"
            :hint="t('shareLink.expiryHint')"
            :label="t('shareLink.expires')"
            persistent-hint
            type="datetime-local"
          />

          <v-text-field
            v-model="password"
            autocomplete="new-password"
            class="mt-2"
            :hint="t('shareLink.passwordHint')"
            :label="t('shareLink.password')"
            persistent-hint
            type="password"
          />

          <v-btn
            class="mt-3"
            color="primary"
            data-testid="share-create-submit"
            :loading="creating"
            type="submit"
          >{{ t('shareLink.create') }}</v-btn>
        </v-form>

        <v-alert
          v-if="error"
          class="mb-3"
          closable
          density="compact"
          type="error"
          @click:close="error = ''"
        >{{ error }}</v-alert>

        <v-progress-linear v-if="loading" indeterminate />
        <p v-else-if="links.length === 0" class="text-body-2" data-testid="share-empty">{{ t('shareLink.none') }}</p>

        <v-list v-else lines="two">
          <template v-for="link in links" :key="link.id">
            <v-list-item :data-testid="`share-link-${link.id}`">
              <v-list-item-title class="d-flex align-center ga-2">
                <v-chip size="small" variant="tonal">{{ link.permission === 'edit' ? t('shareLink.edit') : t('shareLink.view') }}</v-chip>
                <v-chip :color="statusColor(link)" size="small" variant="flat">{{ t(`shareLink.status.${statusOf(link)}`) }}</v-chip>
                <v-icon v-if="link.requires_password" :aria-label="t('shareLink.passwordProtected')" icon="mdi-key-outline" size="small" />
              </v-list-item-title>

              <v-list-item-subtitle>
                {{ link.expires_at ? t('shareLink.expiresAt', { when: formatDate(link.expires_at) }) : t('shareLink.noExpiry') }}
                · {{ t('shareLink.createdAt', { when: formatDate(link.created_at) }) }}
              </v-list-item-subtitle>

              <template #append>
                <v-btn
                  v-if="statusOf(link) === 'active'"
                  :aria-label="t('shareLink.copy')"
                  data-testid="share-copy"
                  icon="mdi-content-copy"
                  size="small"
                  variant="text"
                  @click="copy(link)"
                />

                <v-btn
                  :aria-expanded="logOpen === link.id"
                  :aria-label="t('shareLink.accessLog')"
                  :color="logOpen === link.id ? 'primary' : undefined"
                  data-testid="share-log"
                  icon="mdi-history"
                  size="small"
                  variant="text"
                  @click="toggleLog(link)"
                />

                <v-btn
                  v-if="statusOf(link) !== 'revoked'"
                  :aria-label="t('shareLink.revoke')"
                  color="error"
                  data-testid="share-revoke"
                  icon="mdi-link-off"
                  :loading="revoking === link.id"
                  size="small"
                  variant="text"
                  @click="revoke(link)"
                />
              </template>
            </v-list-item>

            <div v-if="logOpen === link.id" class="px-4 pb-3" :data-testid="`share-log-${link.id}`">
              <v-progress-linear v-if="logLoading" indeterminate />
              <v-alert v-else-if="logError" density="compact" type="error">{{ logError }}</v-alert>
              <p v-else-if="log.length === 0" class="text-body-2">{{ t('shareLink.logEmpty') }}</p>

              <v-table v-else density="compact">
                <thead>
                  <tr>
                    <th>{{ t('shareLink.logWhen') }}</th>
                    <th>{{ t('shareLink.logWho') }}</th>
                    <th>{{ t('shareLink.logAgent') }}</th>
                  </tr>
                </thead>

                <tbody>
                  <tr v-for="entry in log" :key="entry.id">
                    <td>{{ formatDate(entry.accessed_at) }}</td>
                    <td>{{ entry.viewer_user_display_name ? t('shareLink.logMember', { name: entry.viewer_user_display_name, guest: entry.viewer_guest_display_name ?? '' }) : (entry.viewer_guest_display_name ?? t('shareLink.logUnknown')) }}</td>
                    <td class="text-truncate" style="max-width: 200px">{{ entry.user_agent ?? '' }}</td>
                  </tr>
                </tbody>
              </v-table>
            </div>
          </template>
        </v-list>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn @click="open = false">{{ t('shareLink.close') }}</v-btn>
      </v-card-actions>

      <v-snackbar v-model="copied" timeout="2000">{{ t('shareLink.copied') }}</v-snackbar>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import type { ShareLink, ShareLinkAccessLogEntry } from '@/api/generated'
  import { ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { createShareLink, listShareLinkAccessLog, listShareLinks, revokeShareLink } from '@/api/generated'
  import { expiryToIso, shareLinkStatus, shareLinkUrl } from '@/utils/shareLink'

  // Editor+ members only (Decisions 16/24): the parent mounts this solely
  // when canManageShareLinks(page); the access log needs the same role.
  const props = defineProps<{ pageId: string }>()
  const open = defineModel<boolean>({ required: true })

  const { t } = useI18n()

  const links = ref<ShareLink[]>([])
  const loading = ref(false)
  const creating = ref(false)
  const revoking = ref<string | null>(null)
  const error = ref('')
  const copied = ref(false)

  const permission = ref<'view' | 'edit'>('view')
  const expiry = ref('')
  const expiryError = ref('')
  const password = ref('')

  const logOpen = ref<string | null>(null)
  const log = ref<ShareLinkAccessLogEntry[]>([])
  const logLoading = ref(false)
  const logError = ref('')

  function statusOf (link: ShareLink) {
    return shareLinkStatus(link)
  }

  function statusColor (link: ShareLink) {
    return ({ active: 'success', expired: 'warning', revoked: 'error' } as const)[statusOf(link)]
  }

  function formatDate (iso: string) {
    return new Date(iso).toLocaleString()
  }

  async function load () {
    loading.value = true
    error.value = ''
    try {
      const { data, error: apiError } = await listShareLinks({ path: { pageId: props.pageId } })
      if (apiError || !data) {
        throw new Error(apiError?.error ?? t('shareLink.loadFailed'))
      }
      links.value = data
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('shareLink.loadFailed')
    } finally {
      loading.value = false
    }
  }

  watch(open, isOpen => {
    if (isOpen) {
      logOpen.value = null
      load()
    } else {
      // Never keep the typed password around once the dialog is closed.
      password.value = ''
    }
  }, { immediate: true })

  async function create () {
    expiryError.value = ''
    let expiresAt: string | undefined
    if (expiry.value) {
      const iso = expiryToIso(expiry.value)
      if (!iso) {
        expiryError.value = t('shareLink.expiryPast')
        return
      }
      expiresAt = iso
    }
    creating.value = true
    error.value = ''
    try {
      const { error: apiError } = await createShareLink({
        path: { pageId: props.pageId },
        body: {
          permission: permission.value,
          ...(expiresAt ? { expiresAt } : {}),
          ...(password.value ? { password: password.value } : {}),
        },
      })
      if (apiError) {
        throw new Error(apiError.error ?? t('shareLink.createFailed'))
      }
      password.value = ''
      expiry.value = ''
      await load()
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('shareLink.createFailed')
    } finally {
      creating.value = false
    }
  }

  async function revoke (link: ShareLink) {
    revoking.value = link.id
    error.value = ''
    try {
      const { error: apiError } = await revokeShareLink({ path: { shareLinkId: link.id } })
      if (apiError) {
        throw new Error(apiError.error ?? t('shareLink.revokeFailed'))
      }
      await load()
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('shareLink.revokeFailed')
    } finally {
      revoking.value = null
    }
  }

  async function copy (link: ShareLink) {
    try {
      await navigator.clipboard.writeText(shareLinkUrl(window.location.origin, link.token))
      copied.value = true
    } catch {
      error.value = t('shareLink.copyFailed', { url: shareLinkUrl(window.location.origin, link.token) })
    }
  }

  async function toggleLog (link: ShareLink) {
    if (logOpen.value === link.id) {
      logOpen.value = null
      return
    }
    logOpen.value = link.id
    log.value = []
    logError.value = ''
    logLoading.value = true
    try {
      const { data, error: apiError } = await listShareLinkAccessLog({ path: { shareLinkId: link.id } })
      if (apiError || !data) {
        throw new Error(apiError?.error ?? t('shareLink.logFailed'))
      }
      if (logOpen.value === link.id) {
        log.value = data
      }
    } catch (error_) {
      logError.value = error_ instanceof Error ? error_.message : t('shareLink.logFailed')
    } finally {
      logLoading.value = false
    }
  }
</script>
