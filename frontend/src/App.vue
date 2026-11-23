<template>
  <v-app>
    <v-app-bar :elevation="1">
      <v-app-bar-title>{{ t('app.title') }}</v-app-bar-title>
      <v-spacer />

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

    <v-navigation-drawer v-if="authStore.isAuthenticated" permanent>
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

      <template v-if="currentSpaceId">
        <v-divider class="my-2" />
        <v-list-subheader>{{ t('nav.pages') }}</v-list-subheader>
        <PageTreeNav :space-id="currentSpaceId" />
      </template>
    </v-navigation-drawer>

    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>

<script lang="ts" setup>
  import { computed, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRoute, useRouter } from 'vue-router'
  import PageTreeNav from '@/components/PageTreeNav.vue'
  import { useAuthStore } from '@/stores/auth'
  import { useSpacesStore } from '@/stores/spaces'

  const { t } = useI18n()
  const route = useRoute()
  const router = useRouter()
  const authStore = useAuthStore()
  const spacesStore = useSpacesStore()

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
    authStore.logout()
  }
</script>
