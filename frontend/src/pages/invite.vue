<template>
  <v-container class="fill-height" fluid>
    <v-row align="center" justify="center">
      <v-col cols="12" md="4" sm="6">
        <v-card :title="t('redeem.title')">
          <v-card-text>
            <v-progress-linear v-if="state === 'checking'" indeterminate />

            <template v-else-if="state === 'ready'">
              <v-form @submit.prevent="submit">
                <v-text-field
                  v-model="email"
                  autocomplete="username"
                  :label="t('redeem.email')"
                  :readonly="emailLocked"
                  required
                  type="email"
                />

                <v-text-field
                  v-model="displayName"
                  autocomplete="name"
                  :label="t('redeem.displayName')"
                  maxlength="80"
                  required
                />

                <v-text-field
                  v-model="password"
                  autocomplete="new-password"
                  :error-messages="passwordError"
                  :hint="t('redeem.passwordHint')"
                  :label="t('redeem.password')"
                  persistent-hint
                  required
                  type="password"
                />

                <v-alert v-if="error" class="my-3" density="compact" type="error">{{ error }}</v-alert>

                <v-btn
                  block
                  class="mt-2"
                  color="primary"
                  :disabled="!canSubmit"
                  :loading="submitting"
                  type="submit"
                >{{ t('redeem.submit') }}</v-btn>
              </v-form>
            </template>

            <template v-else>
              <v-alert density="compact" type="error">{{ error }}</v-alert>
              <v-btn block class="mt-4" to="/login" variant="tonal">{{ t('redeem.toLogin') }}</v-btn>
            </template>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import { redeemInvite, resolveInvite } from '@/api/generated'
  import { useAuthStore } from '@/stores/auth'
  import { redeemErrorKey } from '@/utils/accounts'
  import { rateLimitMessage, retryAfterSeconds } from '@/utils/rateLimit'

  const props = defineProps<{ token: string }>()
  const { t } = useI18n()
  const router = useRouter()
  const authStore = useAuthStore()

  const state = ref<'checking' | 'ready' | 'unusable'>('checking')
  const email = ref('')
  const emailLocked = ref(false)
  const displayName = ref('')
  const password = ref('')
  const error = ref('')
  const submitting = ref(false)

  const passwordError = computed(() => password.value.length > 0 && (password.value.length < 10 || password.value.length > 128) ? t('redeem.passwordHint') : '')
  const canSubmit = computed(() =>
    email.value.trim() !== '' && displayName.value.trim() !== '' && password.value.length >= 10 && password.value.length <= 128)

  onMounted(async () => {
    let result: Awaited<ReturnType<typeof resolveInvite>>
    try {
      result = await resolveInvite({ path: { token: props.token } })
    } catch {
      state.value = 'unusable'
      error.value = t('redeem.failed')
      return
    }
    const { data, response } = result
    if (data) {
      email.value = data.email ?? ''
      emailLocked.value = data.email !== null
      state.value = 'ready'
      return
    }
    state.value = 'unusable'
    error.value = response?.status === 429
      ? rateLimitMessage(t, retryAfterSeconds(response))
      : t(redeemErrorKey(response?.status ?? 0, 'resolve'))
  })

  async function submit () {
    submitting.value = true
    error.value = ''
    try {
      const { data, error: apiError, response } = await redeemInvite({
        path: { token: props.token },
        body: {
          // A bound invite ignores/forces the address server-side; only send it when the user supplied it.
          ...(emailLocked.value ? {} : { email: email.value.trim() }),
          displayName: displayName.value.trim(),
          password: password.value,
        },
      })
      if (!data) {
        // 409 (email taken) and 400 (password rules) carry a specific server message worth showing.
        const status = response?.status ?? 0
        if (status === 429) {
          error.value = rateLimitMessage(t, retryAfterSeconds(response))
          return
        }
        error.value = (status === 409 || status === 400) && apiError?.error ? apiError.error : t(redeemErrorKey(status, 'redeem'))
        return
      }
      await authStore.loginWithToken(data.token, data.displayName)
      router.replace('/')
    } catch {
      error.value = t('redeem.failed')
    } finally {
      submitting.value = false
    }
  }
</script>
