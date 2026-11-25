<template>
  <v-app>
    <v-app-bar :elevation="1">
      <v-app-bar-title>{{ t('app.title') }}</v-app-bar-title>
      <v-spacer />

      <v-btn
        v-if="showSearch"
        aria-keyshortcuts="Control+K Meta+K"
        :aria-label="t('search.open')"
        data-testid="open-search"
        icon="mdi-magnify"
        :title="t('search.open')"
        @click="searchOpen = true"
      />

      <v-menu location="bottom end">
        <template #activator="{ props: themeMenuProps }">
          <v-btn
            v-bind="themeMenuProps"
            :aria-label="t('theme.label')"
            data-testid="theme-menu"
            :icon="isDark ? 'mdi-weather-night' : 'mdi-weather-sunny'"
            :title="t('theme.label')"
          />
        </template>

        <v-list density="compact" :selected="[preference]" @update:selected="onThemeSelected">
          <v-list-item
            v-for="option in THEME_PREFERENCES"
            :key="option"
            :data-testid="`theme-${option}`"
            :title="t(`theme.${option}`)"
            :value="option"
          />
        </v-list>
      </v-menu>

      <template v-if="authStore.isAuthenticated">
        <v-menu :close-on-content-click="false" location="bottom end">
          <template #activator="{ props: menuProps }">
            <v-btn v-bind="menuProps" class="mr-2" variant="text">
              <v-avatar class="mr-2" color="primary" size="28">
                <v-img v-if="authStore.profile?.avatarUrl" :alt="authStore.displayName ?? ''" :src="authStore.profile.avatarUrl" />
                <span v-else class="text-caption">{{ initials }}</span>
              </v-avatar>
              {{ authStore.displayName }}
            </v-btn>
          </template>

          <v-card min-width="300">
            <v-card-title>{{ authStore.displayName }}</v-card-title>
            <v-card-subtitle v-if="authStore.profile?.email">{{ authStore.profile.email }}</v-card-subtitle>

            <v-card-text>
              <v-chip v-if="authStore.isOrgAdmin" class="mr-1 mb-1" color="primary" size="small">{{ t('profile.admin') }}</v-chip>
              <v-chip v-else class="mr-1 mb-1" size="small">{{ t('profile.editor') }}</v-chip>

              <v-chip v-if="authStore.profile?.authProvider" class="mb-1" size="small" variant="outlined">
                {{ authStore.profile.authProvider === 'oidc' ? t('profile.sso') : t('profile.internal') }}
              </v-chip>

              <div v-if="authStore.profile?.username" class="mt-2 text-body-2">
                {{ t('profile.username') }}: {{ authStore.profile.username }}
              </div>

              <div v-if="authStore.profile?.groups?.length" class="mt-2">
                <div class="text-caption">{{ t('profile.groups') }}</div>

                <v-chip
                  v-for="group in authStore.profile.groups"
                  :key="group"
                  class="mr-1 mt-1"
                  label
                  size="x-small"
                >{{ group }}</v-chip>
              </div>

              <div v-if="authStore.profile?.lastLoginAt" class="mt-2 text-caption">
                {{ t('profile.lastLogin') }}: {{ new Date(authStore.profile.lastLoginAt).toLocaleString() }}
              </div>
            </v-card-text>

            <v-card-actions>
              <v-btn block variant="tonal" @click="logout">{{ t('nav.logout') }}</v-btn>
            </v-card-actions>
          </v-card>
        </v-menu>
      </template>
    </v-app-bar>

    <v-navigation-drawer v-if="authStore.isAuthenticated && !route.meta.guest" permanent>
      <v-list-subheader>{{ t('nav.spaces') }}</v-list-subheader>
      <v-list-item v-if="spacesStore.spaces.length === 0" disabled :title="t('nav.noSpaces')" />

      <v-list-item
        v-for="space in spacesStore.spaces"
        :key="space.id"
        :active="currentSpaceId === space.id"
        :prepend-icon="space.icon ?? 'mdi-folder-outline'"
        :title="space.name"
        :to="`/spaces/${space.id}`"
      />

      <v-list-item
        v-if="authStore.isOrgAdmin"
        data-testid="new-space"
        prepend-icon="mdi-plus"
        :title="t('spaceNav.newSpace')"
        @click="newSpaceOpen = true"
      />

      <template v-if="currentSpaceId">
        <template v-if="canManageSpace">
          <v-divider class="my-2" />
          <v-list-item density="compact" prepend-icon="mdi-account-multiple-outline" :title="t('spaceNav.members')" :to="`/spaces/${currentSpaceId}/members`" />
          <v-list-item density="compact" prepend-icon="mdi-cog-outline" :title="t('spaceNav.settings')" :to="`/spaces/${currentSpaceId}/settings`" />
        </template>

        <v-divider class="my-2" />
        <v-list-subheader>{{ t('nav.pages') }}</v-list-subheader>

        <PageTreeNav :space-id="currentSpaceId">
          <template #menu-items="{ item }">
            <template v-if="canManageTreePages">
              <v-list-item
                data-testid="tree-toggle-lock"
                :prepend-icon="item.locked ? 'mdi-lock-open-variant-outline' : 'mdi-lock-outline'"
                :title="item.locked ? t('pageActions.unlock') : t('pageActions.lock')"
                @click="toggleTreeLock(item.id, item.locked)"
              />

              <v-list-item
                base-color="error"
                data-testid="tree-delete"
                prepend-icon="mdi-delete-outline"
                :title="t('pageActions.delete')"
                @click="askDelete(item.id, item.title)"
              />
            </template>
          </template>
        </PageTreeNav>
      </template>

      <template v-if="authStore.isOrgAdmin" #append>
        <v-divider />
        <v-list-subheader>{{ t('adminNav.section') }}</v-list-subheader>
        <v-list-item density="compact" prepend-icon="mdi-email-plus-outline" :title="t('adminNav.invites')" to="/admin/invites" />
        <v-list-item density="compact" prepend-icon="mdi-account-group-outline" :title="t('adminNav.users')" to="/admin/users" />
        <v-list-item density="compact" prepend-icon="mdi-tune" :title="t('adminNav.settings')" to="/admin/settings" />
      </template>
    </v-navigation-drawer>

    <!-- One shared confirm for every tree row (the row menu unmounts on close). -->
    <ConfirmDialog
      v-if="currentSpaceId"
      v-model="deleteOpen"
      :body="t('pageActions.deleteBody', { title: deleteTarget?.title ?? '' })"
      :confirm-label="t('pageActions.delete')"
      :error="treeError"
      :loading="treeBusy"
      :title="t('pageActions.deleteTitle')"
      @confirm="confirmDelete"
    />

    <v-snackbar v-model="treeErrorShown" color="error">{{ treeError }}</v-snackbar>

    <NewSpaceDialog v-if="authStore.isOrgAdmin" v-model="newSpaceOpen" />

    <SearchDialog v-if="showSearch && currentSpaceId" v-model="searchOpen" :space-id="currentSpaceId" :space-name="currentSpaceName" />

    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>

<script lang="ts" setup>
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRoute, useRouter } from 'vue-router'
  import ConfirmDialog from '@/components/ConfirmDialog.vue'
  import NewSpaceDialog from '@/components/NewSpaceDialog.vue'
  import PageTreeNav from '@/components/PageTreeNav.vue'
  import SearchDialog from '@/components/SearchDialog.vue'
  import { useThemePreference } from '@/composables/useThemePreference'
  import { useAuthStore } from '@/stores/auth'
  import { type PageTreeItem, usePagesStore } from '@/stores/pages'
  import { useSpacesStore } from '@/stores/spaces'
  import { canManagePage, roleAtLeast } from '@/utils/permissions'
  import { isThemePreference, THEME_PREFERENCES } from '@/utils/themePreference'

  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const authStore = useAuthStore()
  const spacesStore = useSpacesStore()
  const pagesStore = usePagesStore()

  const newSpaceOpen = ref(false)
  const searchOpen = ref(false)
  const { preference, isDark, setPreference } = useThemePreference()

  function onThemeSelected (selected: unknown) {
    const next = Array.isArray(selected) ? selected[0] : selected
    if (isThemePreference(next)) {
      setPreference(next)
    }
  }

  // Search is per space and needs a member session: not for share-link guests
  // (route meta guest) and not outside a space.
  const showSearch = computed(() => authStore.isAuthenticated && !route.meta.guest && currentSpaceId.value !== null)
  const currentSpaceName = computed(() => spacesStore.spaces.find(s => s.id === currentSpaceId.value)?.name ?? '')

  // Ctrl/Cmd+K opens search (and takes the browser's own binding).
  function onGlobalKeydown (event: KeyboardEvent) {
    if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'k' && showSearch.value) {
      // Always swallow the browser's own binding, but don't stack on another open dialog.
      if (!searchOpen.value && document.querySelector('.v-overlay--active .v-dialog, .v-overlay--active .v-navigation-drawer--temporary')) {
        event.preventDefault()
        return
      }
      event.preventDefault()
      searchOpen.value = true
    }
  }
  onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
  onBeforeUnmount(() => window.removeEventListener('keydown', onGlobalKeydown))
  const canManageSpace = computed(() =>
    roleAtLeast(spacesStore.spaces.find(s => s.id === currentSpaceId.value)?.my_role, 'admin'))

  // Tree-row lock/delete: same gating as PageActions (editor+ space member,
  // Decision 46); the server enforces it regardless.
  const canManageTreePages = computed(() =>
    canManagePage({ my_role: spacesStore.spaces.find(s => s.id === currentSpaceId.value)?.my_role ?? null }))
  const deleteOpen = ref(false)
  const deleteTarget = ref<{ id: string, title: string } | null>(null)
  const treeBusy = ref(false)
  const treeError = ref('')
  const treeErrorShown = ref(false)

  function showTreeError (error: unknown) {
    treeError.value = error instanceof Error ? error.message : t('pageActions.failed')
    treeErrorShown.value = true
  }

  async function toggleTreeLock (pageId: string, locked: boolean) {
    try {
      await pagesStore.setLocked(pageId, !locked)
    } catch (error) {
      showTreeError(error)
    }
  }

  function askDelete (id: string, title: string) {
    deleteTarget.value = { id, title }
    treeError.value = ''
    deleteOpen.value = true
  }

  // Ids of `rootId` and everything below it (the tree is a flat parent_id list).
  function subtreeIds (nodes: PageTreeItem[], rootId: string): Set<string> {
    const ids = new Set([rootId])
    let grew = true
    while (grew) {
      grew = false
      for (const node of nodes) {
        if (node.parent_id !== null && ids.has(node.parent_id) && !ids.has(node.id)) {
          ids.add(node.id)
          grew = true
        }
      }
    }
    return ids
  }

  async function confirmDelete () {
    const target = deleteTarget.value
    const spaceId = currentSpaceId.value
    if (!target || !spaceId) {
      return
    }
    treeBusy.value = true
    treeError.value = ''
    const openId = typeof route.params.pageId === 'string' ? route.params.pageId : null
    const openPageGone = openId !== null && subtreeIds(pagesStore.treesBySpace[spaceId] ?? [], target.id).has(openId)
    try {
      await pagesStore.deletePage(spaceId, target.id)
      deleteOpen.value = false
      // Leave the page view if the open page was deleted with it (the page
      // itself or a descendant, read from the pre-delete tree).
      if (openPageGone) {
        router.push(`/spaces/${spaceId}`)
      }
    } catch (error) {
      treeError.value = error instanceof Error ? error.message : t('pageActions.failed')
    } finally {
      treeBusy.value = false
    }
  }

  const currentSpaceId = computed(() => {
    const id = route.params.spaceId
    return typeof id === 'string' ? id : null
  })

  watch(
    () => authStore.isAuthenticated,
    (authenticated, wasAuthenticated) => {
      if (authenticated) {
        // Errors surface as the nav's "no spaces" state for now; a failed
        // fetch must not become an unhandled rejection.
        spacesStore.fetchSpaces().catch(() => {})
        return
      }
      // Only on a real logged-in → logged-out transition: explicit logout,
      // or the api client's 401 handler (expired/revoked session), which
      // otherwise left the user on a page whose every request now fails.
      // At startup (wasAuthenticated undefined) the router guard already
      // redirects, and pushing here would race it and drop the deep link.
      if (wasAuthenticated && !route.meta.public) {
        router.push({ path: '/login', query: { redirect: route.fullPath } })
      }
    },
    { immediate: true },
  )

  const initials = computed(() => (authStore.displayName ?? '?')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map(part => part[0]!.toUpperCase())
    .join(''))

  function logout () {
    // Revokes the token server-side; local state is cleared even if that fails.
    authStore.signOut().catch(() => {})
  }
</script>
