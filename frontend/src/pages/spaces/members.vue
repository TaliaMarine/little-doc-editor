<template>
  <v-container v-if="canManage" fluid style="max-width: 900px">
    <h1 class="text-h5 mb-4">{{ t('members.title') }} — {{ space?.name }}</h1>

    <v-alert
      v-if="error"
      class="mb-4"
      closable
      data-testid="members-error"
      density="compact"
      type="error"
      @click:close="error = ''"
    >{{ error }}</v-alert>

    <v-progress-linear v-if="loading" indeterminate />

    <!-- People -->
    <h2 class="text-h6 mt-4 mb-2">{{ t('members.users') }}</h2>

    <v-table v-if="userRows.length > 0" data-testid="user-members">
      <tbody>
        <tr v-for="m in userRows" :key="m.id">
          <td>
            {{ m.user_display_name }}
            <div class="text-caption">{{ m.user_email }}</div>
          </td>

          <td style="width: 180px">
            <v-select
              density="compact"
              :disabled="busy || !canChangeMemberRole(myRole, m.role)"
              hide-details
              :items="roleItems(m.role)"
              :label="t('members.role')"
              :model-value="m.role"
              @update:model-value="role => changeUserRole(m, role)"
            />
          </td>

          <td class="text-right">
            <v-btn
              v-if="canChangeMemberRole(myRole, m.role)"
              :aria-label="`${t('common.remove')}: ${memberName(m)}`"
              :data-testid="`remove-${m.user_id}`"
              :disabled="busy"
              size="small"
              variant="text"
              @click="askRemove(m)"
            >{{ t('common.remove') }}</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>

    <p v-else-if="!loading">{{ t('members.emptyUsers') }}</p>

    <div class="d-flex ga-2 mt-3 align-center">
      <CandidatePicker
        v-model="pickedUser"
        class="flex-grow-1"
        data-testid="user-picker"
        :fetcher="searchUsers"
        :label="t('members.searchUsers')"
      />

      <v-select
        v-model="newUserRole"
        density="compact"
        hide-details
        :items="roleItems()"
        :label="t('members.role')"
        style="max-width: 160px"
      />

      <v-btn color="primary" data-testid="add-user" :disabled="!pickedUser || busy" @click="addUser">{{ t('members.add') }}</v-btn>
    </div>

    <!-- Groups -->
    <h2 class="text-h6 mt-8 mb-2">{{ t('members.groups') }}</h2>
    <p class="text-caption mb-2">{{ t('members.groupNote') }}</p>

    <v-table v-if="groupRows.length > 0" data-testid="group-members">
      <tbody>
        <tr v-for="m in groupRows" :key="m.id">
          <td>{{ m.group_name }}</td>

          <td style="width: 180px">
            <v-select
              density="compact"
              :disabled="busy || !canChangeMemberRole(myRole, m.role)"
              hide-details
              :items="roleItems(m.role)"
              :label="t('members.role')"
              :model-value="m.role"
              @update:model-value="role => changeGroupRole(m, role)"
            />
          </td>

          <td class="text-right">
            <v-btn
              v-if="canChangeMemberRole(myRole, m.role)"
              :aria-label="`${t('common.remove')}: ${memberName(m)}`"
              :data-testid="`remove-group-${m.group_id}`"
              :disabled="busy"
              size="small"
              variant="text"
              @click="askRemove(m)"
            >{{ t('common.remove') }}</v-btn>
          </td>
        </tr>
      </tbody>
    </v-table>

    <p v-else-if="!loading">{{ t('members.emptyGroups') }}</p>

    <div class="d-flex ga-2 mt-3 align-center">
      <CandidatePicker
        v-model="pickedGroup"
        class="flex-grow-1"
        data-testid="group-picker"
        :fetcher="searchGroups"
        :label="t('members.searchGroups')"
        :min-chars="0"
      />

      <v-select
        v-model="newGroupRole"
        density="compact"
        hide-details
        :items="roleItems()"
        :label="t('members.role')"
        style="max-width: 160px"
      />

      <v-btn color="primary" data-testid="add-group" :disabled="!pickedGroup || busy" @click="addGroup">{{ t('members.addGroup') }}</v-btn>
    </div>

    <ConfirmDialog
      :body="t('members.removeConfirm', { name: memberName(toRemove) })"
      :confirm-label="t('common.remove')"
      :loading="busy"
      :model-value="toRemove !== null"
      :title="t('common.remove')"
      @confirm="doRemove"
      @update:model-value="toRemove = null"
    />
  </v-container>

  <v-container v-else fluid>
    <v-progress-linear v-if="spacesStore.loading || !space" indeterminate />
    <v-alert v-else type="warning">{{ t('members.needAdmin') }}</v-alert>
  </v-container>
</template>

<script lang="ts" setup>
  import type { SpaceMember, SpaceRole } from '@/api/generated'
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import {
    addSpaceGroupMember,
    addSpaceMember,
    listSpaceMembers,
    removeSpaceGroupMember,
    removeSpaceMember,
    searchGroupCandidates,
    searchMemberCandidates,
    updateSpaceGroupMember,
    updateSpaceMember,
  } from '@/api/generated'
  import CandidatePicker from '@/components/CandidatePicker.vue'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import { useSpacesStore } from '@/stores/spaces'
  import { assignableRoles, canChangeMemberRole, type Candidate } from '@/utils/accounts'
  import { roleAtLeast } from '@/utils/permissions'

  const props = defineProps<{ spaceId: string }>()
  const { t } = useI18n()
  const spacesStore = useSpacesStore()

  const space = computed(() => spacesStore.spaces.find(s => s.id === props.spaceId))
  const myRole = computed(() => space.value?.my_role)
  const canManage = computed(() => roleAtLeast(myRole.value, 'admin'))

  const members = ref<SpaceMember[]>([])
  const loading = ref(false)
  const busy = ref(false)
  const error = ref('')
  const toRemove = ref<SpaceMember | null>(null)

  const pickedUser = ref<Candidate | null>(null)
  const pickedGroup = ref<Candidate | null>(null)
  const newUserRole = ref<SpaceRole>('editor')
  const newGroupRole = ref<SpaceRole>('editor')

  const userRows = computed(() => members.value.filter(m => m.user_id !== null))
  const groupRows = computed(() => members.value.filter(m => m.group_id !== null))

  // Always include the row's current role so an admin viewing an owner row
  // sees "Owner" (disabled) rather than a blank select.
  function roleItems (current?: SpaceRole) {
    const roles = assignableRoles(myRole.value)
    if (current && !roles.includes(current)) {
      roles.unshift(current)
    }
    return roles.map(value => ({ value, title: t(`members.roles.${value}`) }))
  }

  function memberName (m: SpaceMember | null) {
    return m ? (m.user_display_name ?? m.group_name ?? '') : ''
  }

  async function searchUsers (q: string): Promise<Candidate[]> {
    const { data } = await searchMemberCandidates({ path: { spaceId: props.spaceId }, query: { q } })
    return (data ?? []).map(c => ({ value: c.id, title: c.display_name, subtitle: c.email }))
  }

  async function searchGroups (q: string): Promise<Candidate[]> {
    const { data } = await searchGroupCandidates({ path: { spaceId: props.spaceId }, query: q ? { q } : {} })
    return (data ?? []).map(c => ({ value: c.id, title: c.name }))
  }

  async function load () {
    loading.value = true
    try {
      const { data, error: apiError } = await listSpaceMembers({ path: { spaceId: props.spaceId } })
      if (apiError) {
        throw new Error(apiError.error)
      }
      members.value = data ?? []
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('common.loadFailed')
    } finally {
      loading.value = false
    }
  }

  // Runs a mutation, surfaces the server's message verbatim (last-owner 409,
  // owner-escalation 403 — Decisions 40, 46), then reloads so the table never
  // shows a role the server rejected.
  async function mutate (fn: () => Promise<{ error?: { error: string } | undefined }>) {
    busy.value = true
    error.value = ''
    try {
      const { error: apiError } = await fn()
      if (apiError) {
        error.value = apiError.error
      }
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('members.failed')
    } finally {
      await load()
      busy.value = false
    }
  }

  async function addUser () {
    const picked = pickedUser.value
    if (!picked) {
      return
    }
    await mutate(() => addSpaceMember({ path: { spaceId: props.spaceId }, body: { userId: picked.value, role: newUserRole.value } }))
    if (!error.value) {
      pickedUser.value = null
    }
  }

  async function addGroup () {
    const picked = pickedGroup.value
    if (!picked) {
      return
    }
    await mutate(() => addSpaceGroupMember({ path: { spaceId: props.spaceId }, body: { groupId: picked.value, role: newGroupRole.value } }))
    if (!error.value) {
      pickedGroup.value = null
    }
  }

  function changeUserRole (m: SpaceMember, role: SpaceRole) {
    if (role === m.role || !m.user_id) {
      return
    }
    const userId = m.user_id
    return mutate(() => updateSpaceMember({ path: { spaceId: props.spaceId, userId }, body: { role } }))
  }

  function changeGroupRole (m: SpaceMember, role: SpaceRole) {
    if (role === m.role || !m.group_id) {
      return
    }
    const groupId = m.group_id
    return mutate(() => updateSpaceGroupMember({ path: { spaceId: props.spaceId, groupId }, body: { role } }))
  }

  function askRemove (m: SpaceMember) {
    toRemove.value = m
  }

  async function doRemove () {
    const m = toRemove.value
    if (!m) {
      return
    }
    toRemove.value = null
    await mutate(() => m.user_id
      ? removeSpaceMember({ path: { spaceId: props.spaceId, userId: m.user_id } })
      : removeSpaceGroupMember({ path: { spaceId: props.spaceId, groupId: m.group_id! } }))
  }

  watch(
    [() => props.spaceId, canManage],
    ([, allowed]) => {
      members.value = []
      if (allowed) {
        load()
      }
    },
    { immediate: true },
  )
</script>
