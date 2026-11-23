<template>
  <v-container fluid>
    <h1 class="text-h5 mb-4">{{ space?.name ?? spaceId }}</h1>
    <p class="mb-4">{{ canCreate ? t('space.selectOrCreate') : t('space.select') }}</p>

    <v-alert
      v-if="errorMessage"
      class="mb-4"
      closable
      density="compact"
      type="error"
      @click:close="errorMessage = ''"
    >
      {{ errorMessage }}
    </v-alert>

    <v-btn v-if="canCreate" color="primary" :loading="creating" @click="createRootPage">{{ t('space.newPage') }}</v-btn>
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import { usePagesStore } from '@/stores/pages'
  import { useSpacesStore } from '@/stores/spaces'
  import { roleAtLeast } from '@/utils/permissions'

  const props = defineProps<{ spaceId: string }>()

  const { t } = useI18n()
  const router = useRouter()
  const spacesStore = useSpacesStore()
  const pagesStore = usePagesStore()

  const space = computed(() => spacesStore.spaces.find(s => s.id === props.spaceId))
  // Creating pages needs editor+ (Decision 24). Hidden until the space list
  // has loaded, rather than flashing a button a viewer can't use.
  const canCreate = computed(() => roleAtLeast(space.value?.my_role, 'editor'))
  const creating = ref(false)
  const errorMessage = ref('')

  async function createRootPage () {
    creating.value = true
    errorMessage.value = ''
    try {
      const page = await pagesStore.createPage(props.spaceId, t('space.untitled'), null)
      router.push(`/spaces/${props.spaceId}/pages/${page.id}`)
    } catch (error) {
      errorMessage.value = error instanceof Error ? error.message : t('space.createFailed')
    } finally {
      creating.value = false
    }
  }
</script>
