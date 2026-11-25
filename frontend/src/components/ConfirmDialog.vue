<template>
  <v-dialog v-model="open" max-width="480" persistent>
    <v-card :title="title">
      <v-card-text>
        <p>{{ body }}</p>

        <v-text-field
          v-if="requireText"
          v-model="typed"
          autofocus
          class="mt-4"
          density="compact"
          :label="requireTextLabel"
        />

        <v-alert v-if="error" class="mt-3" density="compact" type="error">{{ error }}</v-alert>
      </v-card-text>

      <v-card-actions>
        <v-spacer />
        <v-btn :disabled="loading" @click="open = false">{{ t('common.cancel') }}</v-btn>

        <v-btn
          color="error"
          data-testid="confirm-action"
          :disabled="requireText !== undefined && typed !== requireText"
          :loading="loading"
          variant="flat"
          @click="emit('confirm')"
        >{{ confirmLabel }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts" setup>
  import { ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'

  defineProps<{
    title: string
    body: string
    confirmLabel: string
    loading?: boolean
    error?: string
    /** When set, the confirm button stays disabled until this exact text is typed. */
    requireText?: string
    requireTextLabel?: string
  }>()
  const open = defineModel<boolean>({ required: true })
  const emit = defineEmits<{ confirm: [] }>()
  const { t } = useI18n()
  const typed = ref('')
  watch(open, () => {
    typed.value = ''
  })
</script>
