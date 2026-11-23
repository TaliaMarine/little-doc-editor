<template>
  <v-container class="fill-height" fluid>
    <v-row align="center" justify="center">
      <v-progress-circular indeterminate />
    </v-row>
  </v-container>
</template>

<script lang="ts" setup>
  import { onMounted } from 'vue'
  import { useRouter } from 'vue-router'
  import { useAuthStore } from '@/stores/auth'

  // Landing page of the OIDC flow: the api redirects here with the session in
  // the URL fragment (#token=…&displayName=…), which never reaches a server log.
  const router = useRouter()
  const authStore = useAuthStore()

  onMounted(async () => {
    const params = new URLSearchParams(window.location.hash.slice(1))
    const token = params.get('token')
    const displayName = params.get('displayName') ?? ''
    // Drop the token from the address bar/history right away.
    history.replaceState(null, '', window.location.pathname)
    const stored = sessionStorage.getItem('little-doc-editor.post-login')
    sessionStorage.removeItem('little-doc-editor.post-login')
    // Only same-app absolute paths are valid post-login targets (no open redirect).
    const target = stored && stored.startsWith('/') && !stored.startsWith('//') ? stored : '/'
    if (!token) {
      await router.replace({ path: '/login', query: { sso_error: '1' } })
      return
    }
    try {
      await authStore.loginWithToken(token, displayName)
      await router.replace(target)
    } catch {
      await router.replace({ path: '/login', query: { sso_error: '1' } })
    }
  })
</script>
