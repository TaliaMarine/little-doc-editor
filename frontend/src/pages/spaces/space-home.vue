<template>
  <v-container fluid>
    <h1 class="text-h5 mb-4">{{ space?.name ?? spaceId }}</h1>
    <p class="mb-4">{{ canCreate ? t('space.selectOrCreate') : t('space.select') }}</p>

    <div class="d-flex flex-wrap ga-2">
      <v-btn v-if="canCreate" color="primary" data-testid="new-page" @click="newPageOpen = true">{{ t('space.newPage') }}</v-btn>
      <ExportMenu v-if="space" :space-id="spaceId" />
      <v-btn v-if="canManage" prepend-icon="mdi-account-multiple-outline" :to="`/spaces/${spaceId}/members`" variant="outlined">{{ t('spaceNav.members') }}</v-btn>
      <v-btn v-if="canManage" prepend-icon="mdi-cog-outline" :to="`/spaces/${spaceId}/settings`" variant="outlined">{{ t('spaceNav.settings') }}</v-btn>
    </div>

    <NewPageDialog v-if="canCreate" v-model="newPageOpen" :parent-id="null" :space-id="spaceId" />
  </v-container>
</template>

<script lang="ts" setup>
  import { computed, ref } from 'vue'
  import { useI18n } from 'vue-i18n'
  import ExportMenu from '@/components/ExportMenu.vue'
  import NewPageDialog from '@/components/NewPageDialog.vue'
  import { useSpacesStore } from '@/stores/spaces'
  import { roleAtLeast } from '@/utils/permissions'

  const props = defineProps<{ spaceId: string }>()

  const { t } = useI18n()
  const spacesStore = useSpacesStore()

  const space = computed(() => spacesStore.spaces.find(s => s.id === props.spaceId))
  // Creating pages needs editor+ (Decision 24). Hidden until the space list
  // has loaded, rather than flashing a button a viewer can't use.
  const canCreate = computed(() => roleAtLeast(space.value?.my_role, 'editor'))
  const canManage = computed(() => roleAtLeast(space.value?.my_role, 'admin'))
  const newPageOpen = ref(false)
</script>
