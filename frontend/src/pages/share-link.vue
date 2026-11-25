<template>
  <v-container class="share-page" data-testid="share-page">
    <v-progress-linear v-if="phase === 'loading'" indeterminate />

    <v-alert v-else-if="phase === 'error'" data-testid="share-error" type="error">{{ errorText }}</v-alert>

    <v-card v-else-if="phase === 'prompt'" class="mx-auto" max-width="480">
      <v-form data-testid="share-prompt" @submit.prevent="enter">
        <v-card-title>{{ resolved?.pageTitle ?? t('guest.sharedPage') }}</v-card-title>
        <v-card-subtitle>{{ resolved?.permission === 'edit' ? t('guest.canEdit') : t('guest.viewOnly') }}</v-card-subtitle>

        <v-card-text>
          <p class="mb-4">{{ t('guest.namePrompt') }}</p>

          <v-text-field
            v-model="displayName"
            autofocus
            data-testid="share-name"
            :error-messages="nameError"
            :label="t('guest.displayName')"
            maxlength="80"
            required
          />

          <v-text-field
            v-if="resolved?.requiresPassword"
            v-model="password"
            autocomplete="off"
            data-testid="share-password"
            :error-messages="passwordError"
            :label="t('guest.password')"
            type="password"
          />
        </v-card-text>

        <v-card-actions>
          <v-spacer />
          <v-btn color="primary" data-testid="share-enter" :loading="entering" type="submit">{{ t('guest.continue') }}</v-btn>
        </v-card-actions>
      </v-form>
    </v-card>

    <template v-else-if="phase === 'ready'">
      <div class="d-flex align-center mb-2 ga-2">
        <v-btn
          v-if="showBack"
          data-testid="share-back"
          prepend-icon="mdi-arrow-left"
          size="small"
          :to="`/share/${encodeURIComponent(token)}`"
          variant="text"
        >{{ t('guest.backToShared') }}</v-btn>

        <v-chip data-testid="share-identity" size="small" variant="tonal">{{ t('guest.viewingAs', { name: guestStore.session?.displayName ?? '' }) }}</v-chip>
      </div>

      <v-alert v-if="pageError" density="compact" type="error">{{ pageError }}</v-alert>
      <v-progress-linear v-else-if="!pagesStore.currentPage" indeterminate />
      <!-- Guests get no history, share dialog or space tree: PageEditor hides
           them because `guest` is set (and the api refuses them anyway). -->
      <div v-else @click="followContentLink">
        <PageEditor :key="pagesStore.currentPage.id" :guest="{ token: guestStore.session?.token ?? '', displayName: guestStore.session?.displayName ?? '' }" :page="pagesStore.currentPage" />
      </div>
    </template>
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import PageEditor from '@/components/PageEditor.vue'
  import { type ResolvedLink, ShareError, useGuestStore } from '@/stores/guest'
  import { usePagesStore } from '@/stores/pages'
  import { rateLimitMessage } from '@/utils/rateLimit'
  import { sharePathForHref } from '@/utils/shareLink'

  // Public route /share/:token[/:pageId] (no auth guard). pageId is a
  // descendant of the shared page (Decision 21); the api decides whether the
  // guest session covers it.
  const props = defineProps<{ token: string, pageId?: string }>()

  const { t } = useI18n()
  const router = useRouter()
  const guestStore = useGuestStore()
  const pagesStore = usePagesStore()

  const phase = ref<'loading' | 'error' | 'prompt' | 'ready'>('loading')
  const errorText = ref('')
  const resolved = ref<ResolvedLink | null>(null)
  const displayName = ref('')
  const password = ref('')
  const nameError = ref('')
  const passwordError = ref('')
  const entering = ref(false)
  const pageError = ref('')

  const targetPageId = computed(() => props.pageId ?? guestStore.session?.pageId ?? '')
  const showBack = computed(() => !!props.pageId && props.pageId !== guestStore.session?.pageId)

  function messageFor (error: unknown): string {
    const kind = error instanceof ShareError ? error.kind : 'network'
    if (kind === 'rateLimited') {
      return rateLimitMessage(t, error instanceof ShareError ? error.retryAfter : null)
    }
    return t(`guest.errors.${kind}`)
  }

  async function start () {
    phase.value = 'loading'
    pagesStore.currentPage = null
    guestStore.deactivate()
    guestStore.expired = false
    if (guestStore.restore(props.token)) {
      phase.value = 'ready'
      return
    }
    try {
      resolved.value = await guestStore.resolve(props.token)
      phase.value = 'prompt'
    } catch (error) {
      errorText.value = messageFor(error)
      phase.value = 'error'
    }
  }

  // The display name is required before anything is shown or editable
  // (Decision 6) — the prompt is the only way into the 'ready' phase.
  async function enter () {
    nameError.value = ''
    passwordError.value = ''
    if (!displayName.value.trim()) {
      nameError.value = t('guest.errors.nameRequired')
      return
    }
    entering.value = true
    try {
      await guestStore.redeem(props.token, displayName.value, password.value)
      password.value = ''
      phase.value = 'ready'
    } catch (error) {
      if (error instanceof ShareError && error.kind === 'wrongPassword') {
        passwordError.value = t('guest.errors.wrongPassword')
      } else if (error instanceof ShareError && (error.kind === 'gone' || error.kind === 'notFound')) {
        errorText.value = messageFor(error)
        phase.value = 'error'
      } else {
        nameError.value = messageFor(error)
      }
    } finally {
      entering.value = false
    }
  }

  watch(() => props.token, start, { immediate: true })

  // The api refused the guest token (link revoked/expired mid-session): the
  // stored session is already cleared, so a reload re-resolves the link.
  watch(() => guestStore.expired, ended => {
    if (ended) {
      errorText.value = t('guest.sessionEnded')
      phase.value = 'error'
    }
  })

  watch([() => phase.value, targetPageId], async ([current, id]) => {
    if (current !== 'ready' || !id) {
      return
    }
    pageError.value = ''
    try {
      await pagesStore.fetchPage(id)
    } catch (error) {
      pageError.value = error instanceof Error ? error.message : t('editor.loadFailed')
    }
  }, { immediate: true })

  // In-app links to other pages of the shared tree stay inside the share
  // route; anything else is left to the browser.
  function followContentLink (event: MouseEvent) {
    const anchor = (event.target as HTMLElement | null)?.closest('a[href]')
    if (!anchor || event.defaultPrevented || event.ctrlKey || event.metaKey || event.shiftKey) {
      return
    }
    const path = sharePathForHref(anchor.getAttribute('href') ?? '', window.location.origin, props.token)
    if (path) {
      event.preventDefault()
      router.push(path)
    }
  }

  // Leaving the route drops the guest token from the api client (the member
  // session, if any, is untouched).
  onBeforeUnmount(() => {
    guestStore.deactivate()
    pagesStore.currentPage = null
  })
</script>
