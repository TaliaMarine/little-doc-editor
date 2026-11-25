import type { Page as ApiPage, PageTreeItem as ApiPageTreeItem } from '@/api/generated'
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createPage as apiCreatePage, deletePage as apiDeletePage, movePage as apiMovePage, getPage, listPagesBySpace, setPageLocked, updatePageContent } from '@/api/generated'

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
  // Bumped to force the open editor to remount with a fresh live document
  // (after a revision restore replaced the content server-side).
  const editorEpoch = ref(0)

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

  // Title only: `content` is omitted so the server leaves it alone. While a
  // page is open in the live editor, content is owned by collab (Yjs) — a PUT
  // carrying content would clobber live keystrokes.
  async function renamePage (pageId: string, title: string) {
    const { data, error } = await updatePageContent({ path: { pageId }, body: { title } })
    if (error) {
      throw new Error(error.error ?? 'Failed to save page')
    }
    if (currentPage.value?.id === pageId && data) {
      currentPage.value = { ...currentPage.value, title: data.title }
    }
    return data
  }

  // With `templateKey` the server builds content (and the default title when
  // `title` is empty); the body then must not carry `content`.
  async function createPage (spaceId: string, title: string, parentId: string | null, templateKey?: string) {
    const { data, error } = await apiCreatePage({
      path: { spaceId },
      body: templateKey
        ? { title: title || undefined, parentId, templateKey }
        : { title, parentId, content: { type: 'doc', content: [] } },
    })
    if (error) {
      throw new Error(error.error ?? 'Failed to create page')
    }
    await fetchTree(spaceId)
    return data
  }

  async function deletePage (spaceId: string, pageId: string) {
    const { error } = await apiDeletePage({ path: { pageId } })
    if (error) {
      throw new Error(error.error ?? 'Failed to delete page')
    }
    if (currentPage.value?.id === pageId) {
      currentPage.value = null
    }
    await fetchTree(spaceId)
  }

  async function setLocked (pageId: string, locked: boolean) {
    const { data, error } = await setPageLocked({ path: { pageId }, body: { locked } })
    if (error) {
      throw new Error(error.error ?? 'Failed to change lock')
    }
    if (currentPage.value?.id === pageId && data) {
      currentPage.value = { ...currentPage.value, locked: data.locked }
    }
    if (data) {
      await fetchTree(data.space_id)
    }
    return data
  }

  // Reparent/reorder (editor+, members only). `position` is computed
  // client-side (utils/pageMove.ts); the tree is refetched so every row's
  // position stays authoritative.
  async function movePage (spaceId: string, pageId: string, parentId: string | null, position: number) {
    const { error } = await apiMovePage({ path: { pageId }, body: { parentId, position } })
    if (error) {
      throw new Error(error.error ?? 'Failed to move page')
    }
    await fetchTree(spaceId)
  }

  return { editorEpoch, movePage, deletePage, setLocked, treesBySpace, treeLoading, pageLoading, currentPage, fetchTree, fetchPage, savePage, renamePage, createPage }
})
