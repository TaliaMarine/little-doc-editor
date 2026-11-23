<template>
  <v-app>
    <v-app-bar :elevation="1">
      <v-app-bar-title>{{ t('app.title') }}</v-app-bar-title>
      <v-spacer />

      <template v-if="authStore.isAuthenticated">
        <span class="mr-4">{{ authStore.displayName }}</span>
        <v-btn variant="text" @click="logout">{{ t('nav.logout') }}</v-btn>
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

  function logout () {
    authStore.logout()
  }
</script>
