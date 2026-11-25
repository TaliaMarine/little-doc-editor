<template>
  <v-container class="fill-height" fluid>
    <v-row align="center" justify="center">
      <v-col cols="12" md="4" sm="6">
        <v-card>
          <v-card-title>{{ t('login.title') }}</v-card-title>

          <v-card-text>
            <v-form @submit.prevent="submit">
              <v-text-field
                v-model="email"
                autocomplete="username"
                :label="t('login.email')"
                required
                type="email"
              />

              <v-text-field
                v-model="password"
                autocomplete="current-password"
                :label="t('login.password')"
                required
                type="password"
              />

              <v-alert v-if="errorMessage" class="mb-4" density="compact" type="error">{{ errorMessage }}</v-alert>
              <v-btn block color="primary" :loading="submitting" type="submit">{{ t('login.submit') }}</v-btn>

              <v-btn
                v-if="oidcEnabled"
                block
                class="mt-2"
                variant="outlined"
                @click="ssoLogin"
              >{{ t('login.sso') }}</v-btn>
            </v-form>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRoute, useRouter } from 'vue-router'
  import { getOidcStatus } from '@/api/generated'
  import { useAuthStore } from '@/stores/auth'
  import { RateLimitedError, rateLimitMessage } from '@/utils/rateLimit'
  import { safeRedirect } from '@/utils/redirect'

  const { t } = useI18n()
  const router = useRouter()
  const route = useRoute()
  const authStore = useAuthStore()

  const email = ref('')
  const password = ref('')
  const submitting = ref(false)
  const errorMessage = ref('')

  // `reason` comes from the api's SSO error redirect (Decision 39); anything
  // unknown falls back to the generic message.
  function ssoErrorMessage (reason: unknown): string {
    switch (reason) {
      case 'email_taken': { return t('login.ssoEmailTaken') }
      case 'provider_error': { return t('login.ssoProviderError') }
      default: { return t('login.ssoFailed') }
    }
  }

  const oidcEnabled = ref(false)
  onMounted(async () => {
    // Without a reachable status endpoint the SSO button simply stays hidden.
    const { data } = await getOidcStatus().catch(() => ({ data: undefined }))
    oidcEnabled.value = data?.enabled ?? false
    if (route.query.sso_error) {
      // The SSO round trip failed: a stale marker must not authorise a later
      // forged /auth/callback.
      sessionStorage.removeItem('little-doc-editor.post-login')
      errorMessage.value = ssoErrorMessage(route.query.reason)
    }
  })

  function ssoLogin () {
    // Remember where to go after the round trip; the callback page reads it.
    const redirect = safeRedirect(route.query.redirect)
    sessionStorage.setItem('little-doc-editor.post-login', redirect)
    window.location.assign('/api/v1/auth/oidc/login')
  }

  async function submit () {
    submitting.value = true
    errorMessage.value = ''
    try {
      await authStore.login(email.value, password.value)
      router.push(safeRedirect(route.query.redirect))
    } catch (error) {
      errorMessage.value = error instanceof RateLimitedError
        ? rateLimitMessage(t, error.retryAfter)
        : (error instanceof Error ? error.message : t('login.error'))
    } finally {
      submitting.value = false
    }
  }
</script>
