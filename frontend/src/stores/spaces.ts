import type { Space as ApiSpace } from '@/api/generated'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createSpace as apiCreateSpace, deleteSpace as apiDeleteSpace, updateSpace as apiUpdateSpace, listSpaces } from '@/api/generated'

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

  async function createSpace (body: { name: string, slug: string, icon?: string }) {
    const { data, error } = await apiCreateSpace({ body })
    if (error) {
      throw new Error(error.error ?? 'Failed to create space')
    }
    await fetchSpaces()
    return data!
  }

  async function updateSpace (spaceId: string, body: { name?: string, icon?: string | null, historyMaxCount?: number | null, historyMaxAgeDays?: number | null }) {
    const { data, error } = await apiUpdateSpace({ path: { spaceId }, body })
    if (error) {
      throw new Error(error.error ?? 'Failed to update space')
    }
    await fetchSpaces()
    return data!
  }

  async function deleteSpace (spaceId: string) {
    const { error } = await apiDeleteSpace({ path: { spaceId } })
    if (error) {
      throw new Error(error.error ?? 'Failed to delete space')
    }
    spaces.value = spaces.value.filter(s => s.id !== spaceId)
  }

  return { spaces, loading, fetchSpaces, createSpace, updateSpace, deleteSpace }
})
