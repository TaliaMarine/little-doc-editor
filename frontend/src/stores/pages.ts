import type { Page as ApiPage, PageTreeItem as ApiPageTreeItem } from '@/api/generated'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createPage as apiCreatePage, getPage, listPagesBySpace, updatePageContent } from '@/api/generated'

export type PageTreeItem = ApiPageTreeItem
export type Page = ApiPage

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
      const { data, error } = await listPagesBySpace({ path: { spaceId } })
      if (error) {
        throw new Error(error.error ?? 'Failed to load pages')
      }
      treesBySpace.value[spaceId] = data ?? []
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
      const { data, error } = await getPage({ path: { pageId } })
      if (error) {
        throw new Error(error.error ?? 'Failed to load page')
      }
      currentPage.value = data ?? null
      return data
    } finally {
      pageLoading.value = false
    }
  }

  async function savePage (pageId: string, title: string, content: Record<string, unknown>) {
    const { data, error } = await updatePageContent({
      path: { pageId },
      body: { title, content },
    })
    if (error) {
      throw new Error(error.error ?? 'Failed to save page')
    }
    currentPage.value = data ?? null
    return data
  }

  async function createPage (spaceId: string, title: string, parentId: string | null) {
    const { data, error } = await apiCreatePage({
      path: { spaceId },
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
