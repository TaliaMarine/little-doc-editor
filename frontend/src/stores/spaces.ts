import type { Space as ApiSpace } from '@/api/generated'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { listSpaces } from '@/api/generated'

export type Space = ApiSpace

export const useSpacesStore = defineStore('spaces', () => {
  const spaces = ref<Space[]>([])
  const loading = ref(false)

  async function fetchSpaces () {
    loading.value = true
    try {
      const { data, error } = await listSpaces()
      if (error) {
        throw new Error(error.error ?? 'Failed to load spaces')
      }
      spaces.value = data ?? []
    } finally {
      loading.value = false
    }
  }

  return { spaces, loading, fetchSpaces }
})
