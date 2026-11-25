<template>
  <v-dialog v-model="open" max-width="440">
    <v-card :title="t('spaceNew.title')">
      <v-form @submit.prevent="submit">
        <v-card-text>
          <v-text-field v-model="name" autofocus :label="t('spaceNew.name')" required />

          <v-text-field
            v-model="slug"
            :hint="t('spaceNew.slugHint')"
            :label="t('spaceNew.slug')"
            persistent-hint
            required
          />

          <v-text-field v-model="icon" class="mt-2" :label="t('spaceNew.icon')" />
          <v-alert v-if="error" class="mt-2" density="compact" type="error">{{ error }}</v-alert>
        </v-card-text>

        <v-card-actions>
          <v-spacer />
          <v-btn @click="open = false">{{ t('common.cancel') }}</v-btn>

          <v-btn
            color="primary"
            :disabled="!name.trim() || !slug.trim()"
            :loading="saving"
            type="submit"
            variant="flat"
          >{{ t('spaceNew.create') }}</v-btn>
        </v-card-actions>
      </v-form>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import { ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useRouter } from 'vue-router'
  import { useSpacesStore } from '@/stores/spaces'
  import { slugify } from '@/utils/accounts'

  const open = defineModel<boolean>({ required: true })
  const { t } = useI18n()
  const router = useRouter()
  const spacesStore = useSpacesStore()

  const name = ref('')
  const slug = ref('')
  const icon = ref('')
  const saving = ref(false)
  const error = ref('')
  let slugTouched = false

  watch(open, isOpen => {
    if (isOpen) {
      name.value = slug.value = icon.value = error.value = ''
      slugTouched = false
    }
  })
  watch(name, value => {
    if (!slugTouched) {
      slug.value = slugify(value)
    }
  })
  watch(slug, value => {
    if (value !== slugify(name.value)) {
      slugTouched = true
    }
  })

  async function submit () {
    saving.value = true
    error.value = ''
    try {
      const space = await spacesStore.createSpace({
        name: name.value.trim(),
        slug: slug.value.trim(),
        ...(icon.value.trim() ? { icon: icon.value.trim() } : {}),
      })
      open.value = false
      router.push(`/spaces/${space.id}`)
    } catch (error_) {
      error.value = error_ instanceof Error ? error_.message : t('spaceNew.failed')
    } finally {
      saving.value = false
    }
  }
</script>
