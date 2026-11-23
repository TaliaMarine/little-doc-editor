import type { components } from '@/api/schema'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'

export type Space = components['schemas']['Space']

export const useSpacesStore = defineStore('spaces', () => {
  const spaces = ref<Space[]>([])
  const loading = ref(false)

  async function fetchSpaces () {
    loading.value = true
    try {
      const { data, error } = await api.GET('/spaces')
      if (error) {
        throw new Error(error.error ?? 'Failed to load spaces')
      }
      spaces.value = data
    } finally {
      loading.value = false
    }
  }

  return { spaces, loading, fetchSpaces }
})
