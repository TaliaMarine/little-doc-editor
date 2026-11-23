<template>
  <div>
    <v-list-item
      v-for="item in orderedItems"
      :key="item.id"
      density="compact"
      :style="{ paddingLeft: `${16 + item.depth * 16}px` }"
      :title="item.title"
      :to="`/spaces/${spaceId}/pages/${item.id}`"
    />

    <v-list-item v-if="loadError" density="compact" disabled :title="loadError" />
    <v-list-item v-else-if="orderedItems.length === 0" density="compact" disabled :title="t('nav.noPages')" />
  </div>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { type PageTreeItem, usePagesStore } from '@/stores/pages'

  const props = defineProps<{ spaceId: string }>()

  const { t } = useI18n()
  const pagesStore = usePagesStore()
  const loadError = ref('')

  watch(() => props.spaceId, async id => {
    loadError.value = ''
    try {
      await pagesStore.fetchTree(id)
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : t('nav.loadFailed')
    }
  }, { immediate: true })

  // Renders the tree flattened with indentation-by-depth rather than
  // collapsible nested groups — simplest correct rendering for now;
  // collapse/expand and drag-and-drop reordering (SPEC.md §5) are follow-up
  // work once this baseline nav is in place.
  //
  // Children are grouped by parent in one pass (the API already returns
  // pages ordered by position, so grouping preserves sibling order) rather
  // than re-filtering the whole list once per node, which was O(n²).
  const orderedItems = computed(() => {
    const byParent = new Map<string | null, PageTreeItem[]>()
    for (const item of pagesStore.treesBySpace[props.spaceId] ?? []) {
      const siblings = byParent.get(item.parent_id)
      if (siblings) {
        siblings.push(item)
      } else {
        byParent.set(item.parent_id, [item])
      }
    }

    const result: (PageTreeItem & { depth: number })[] = []
    const visited = new Set<string>()
    function walk (parentId: string | null, depth: number) {
      for (const item of byParent.get(parentId) ?? []) {
        // Defensive: a parent_id cycle would otherwise recurse forever.
        if (visited.has(item.id)) continue
        visited.add(item.id)
        result.push({ ...item, depth })
        walk(item.id, depth + 1)
      }
    }
    walk(null, 0)
    return result
  })
</script>
