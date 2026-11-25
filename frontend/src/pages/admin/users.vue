<template>
  <v-container v-if="authStore.isOrgAdmin" fluid>
    <h1 class="text-h5 mb-4">{{ t('users.title') }}</h1>

    <v-text-field
      v-model="query"
      class="mb-2"
      clearable
      density="compact"
      hide-details
      :label="t('users.search')"
      prepend-inner-icon="mdi-magnify"
      style="max-width: 360px"
    />

    <v-alert
      v-if="error"
      class="my-2"
      closable
      density="compact"
      type="error"
      @click:close="error = ''"
    >{{ error }}</v-alert>

    <v-progress-linear v-if="loading" indeterminate />

    <v-table v-if="users.length > 0" class="mt-2" data-testid="user-table">
      <thead>
        <tr>
          <th>{{ t('users.colName') }}</th>
          <th>{{ t('users.colEmail') }}</th>
          <th>{{ t('users.colProvider') }}</th>
          <th>{{ t('users.colLastLogin') }}</th>
          <th>{{ t('users.colStatus') }}</th>
          <th />
        </tr>
      </thead>

      <tbody>
        <tr v-for="user in users" :key="user.id">
          <td>
            {{ user.display_name }}
            <v-chip v-if="user.is_org_admin" class="ml-1" color="primary" size="x-small">{{ t('users.admin') }}</v-chip>
          </td>

          <td>{{ user.email }}</td>
          <td>{{ user.auth_provider === 'oidc' ? t('users.oidc') : t('users.internal') }}</td>
          <td>{{ user.last_login_at ? new Date(user.last_login_at).toLocaleString() : t('users.never') }}</td>

          <td>
            <v-chip :color="user.disabled ? 'error' : 'success'" size="small" variant="tonal">{{ user.disabled ? t('users.disabled') : t('users.active') }}</v-chip>
          </td>

          <td class="text-right">
            <v-btn
              v-if="user.email !== authStore.profile?.email"
              :aria-label="`${user.disabled ? t('users.enable') : t('users.disable')}: ${user.display_name}`"
              :data-testid="`toggle-${user.id}`"
              :loading="busyId === user.id"
              size="small"
              variant="text"
              @click="toggle(user)"
            >{{ user.disabled ? t('users.enable') : t('users.disable') }}</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>

    <p v-else-if="!loading" class="mt-2">{{ t('users.empty') }}</p>
  </v-container>

  <v-container v-else fluid><v-alert type="warning">{{ t('common.noAccess') }}</v-alert></v-container>
</template>

<script lang="ts" setup>
  import type { User } from '@/api/generated'
  import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { listUsers, setUserDisabled } from '@/api/generated'
  import { useAuthStore } from '@/stores/auth'

  const { t } = useI18n()
  const authStore = useAuthStore()

  const users = ref<User[]>([])
  const loading = ref(false)
  const error = ref('')
  const query = ref('')
  const busyId = ref<string | null>(null)

  let seq = 0
  async function load () {
    const mine = ++seq
    loading.value = true
    try {
      const { data, error: apiError } = await listUsers({ query: query.value ? { q: query.value } : {} })
      if (mine !== seq) {
        return
      }
      if (apiError) {
        throw new Error(apiError.error)
      }
      users.value = data ?? []
    } catch (error_) {
      if (mine === seq) {
        error.value = error_ instanceof Error ? error_.message : t('common.loadFailed')
      }
    } finally {
      if (mine === seq) {
        loading.value = false
      }
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined
  watch(query, () => {
    clearTimeout(timer)
    timer = setTimeout(load, 250)
  })
  onBeforeUnmount(() => clearTimeout(timer))

  async function toggle (user: User) {
    busyId.value = user.id
    error.value = ''
    try {
      const { error: apiError } = await setUserDisabled({ path: { userId: user.id }, body: { disabled: !user.disabled } })
      if (apiError) {
        throw new Error(apiError.error)
      }
      await load()
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('users.failed')
    } finally {
      busyId.value = null
    }
  }

  onMounted(() => {
    if (authStore.isOrgAdmin) {
      load()
    }
  })
</script>
