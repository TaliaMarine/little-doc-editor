import type { components } from '@/api/schema'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/api/client'

export type PageTreeItem = components['schemas']['PageTreeItem']
export type Page = components['schemas']['Page']

export const usePagesStore = defineStore('pages', () => {
  // Keyed by spaceId: the tree nav only ever shows one space at a time, but
  // caching per-space avoids re-fetching when navigating back to a space
  // already visited this session.
  const treesBySpace = ref<Record<string, PageTreeItem[]>>({})
  // Separate flags: a tree refresh (e.g. after creating a page) shouldn't
  // make the page view think its own page is loading, and vice versa.
  const treeLoading = ref(false)
  const pageLoading = ref(false)
  const currentPage = ref<Page | null>(null)

  async function fetchTree (spaceId: string) {
    treeLoading.value = true
    try {
      const { data, error } = await api.GET('/spaces/{spaceId}/pages', {
        params: { path: { spaceId } },
      })
      if (error) {
        throw new Error(error.error ?? 'Failed to load pages')
      }
      treesBySpace.value[spaceId] = data
    } finally {
      treeLoading.value = false
    }
  }

  async function fetchPage (pageId: string) {
    pageLoading.value = true
    // Drop a different page that's still displayed — otherwise a failed
    // fetch (403/404) leaves the previous page on screen under the new URL.
    if (currentPage.value?.id !== pageId) {
      currentPage.value = null
    }
    try {
      const { data, error } = await api.GET('/pages/{pageId}', {
        params: { path: { pageId } },
      })
      if (error) {
        throw new Error(error.error ?? 'Failed to load page')
      }
      currentPage.value = data
      return data
    } finally {
      pageLoading.value = false
    }
  }

  async function savePage (pageId: string, title: string, content: Record<string, unknown>) {
    const { data, error } = await api.PUT('/pages/{pageId}/content', {
      params: { path: { pageId } },
      body: { title, content },
    })
    if (error) {
      throw new Error(error.error ?? 'Failed to save page')
    }
    currentPage.value = data
    return data
  }

  async function createPage (spaceId: string, title: string, parentId: string | null) {
    const { data, error } = await api.POST('/spaces/{spaceId}/pages', {
      params: { path: { spaceId } },
      body: { title, parentId, content: { type: 'doc', content: [] } },
    })
    if (error) {
      throw new Error(error.error ?? 'Failed to create page')
    }
    await fetchTree(spaceId)
    return data
  }

  return { treesBySpace, treeLoading, pageLoading, currentPage, fetchTree, fetchPage, savePage, createPage }
})
