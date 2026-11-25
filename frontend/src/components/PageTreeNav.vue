<template>
  <div>
    <v-list-item
      v-for="item in rows"
      :key="item.id"
      :class="rowClasses(item.id)"
      :data-page-id="item.id"
      :data-testid="`tree-row-${item.id}`"
      density="compact"
      :draggable="canMove"
      :style="{ paddingLeft: `${8 + item.depth * 16}px` }"
      :title="item.title"
      :to="`/spaces/${spaceId}/pages/${item.id}`"
      @dragend="endDrag"
      @dragover="overRow($event, item.id)"
      @dragstart="startDrag($event, item.id)"
      @drop="dropOnRow($event, item.id)"
    >
      <template #prepend>
        <v-icon
          v-if="canMove"
          aria-hidden="true"
          class="tree-handle mr-1"
          data-testid="tree-handle"
          icon="mdi-drag"
          size="small"
        />

        <v-btn
          v-if="item.hasChildren"
          :aria-expanded="!collapsed.has(item.id)"
          :aria-label="collapsed.has(item.id) ? t('tree.expand', { title: item.title }) : t('tree.collapse', { title: item.title })"
          class="mr-1"
          data-testid="tree-toggle"
          :icon="collapsed.has(item.id) ? 'mdi-chevron-right' : 'mdi-chevron-down'"
          size="x-small"
          variant="text"
          @click.prevent.stop="toggle(item.id)"
        />

        <span v-else class="tree-spacer mr-1" />
      </template>

      <template v-if="canMove || $slots['menu-items']" #append>
        <v-menu>
          <template #activator="{ props: menuProps }">
            <v-btn
              v-bind="menuProps"
              :aria-label="t('tree.actions', { title: item.title })"
              data-testid="tree-menu"
              icon="mdi-dots-vertical"
              size="x-small"
              variant="text"
              @click.prevent.stop
            />
          </template>

          <v-list density="compact">
            <template v-if="canMove">
              <v-list-item
                data-testid="tree-move-up"
                :disabled="!planStep(items, item.id, 'up')"
                prepend-icon="mdi-arrow-up"
                :title="t('tree.moveUp')"
                @click="moveStep(item.id, 'up')"
              />

              <v-list-item
                data-testid="tree-move-down"
                :disabled="!planStep(items, item.id, 'down')"
                prepend-icon="mdi-arrow-down"
                :title="t('tree.moveDown')"
                @click="moveStep(item.id, 'down')"
              />

              <v-list-item
                data-testid="tree-move-to"
                prepend-icon="mdi-file-move-outline"
                :title="t('tree.moveTo')"
                @click="openMoveTo(item.id)"
              />
            </template>

            <!--
              Extension point: per-page menu entries owned by other features
              (delete, lock, …). They are responsible for their own role gating.
            -->
            <slot :item="item" name="menu-items" />
          </v-list>
        </v-menu>
      </template>
    </v-list-item>

    <v-list-item v-if="loadError" density="compact" disabled :title="loadError" />
    <v-list-item v-else-if="rows.length === 0" density="compact" disabled :title="t('nav.noPages')" />

    <v-dialog v-model="moveToOpen" max-width="420">
      <v-card :title="t('tree.moveToTitle', { title: moveToTitle })">
        <v-card-text>
          <v-select
            v-model="moveToParent"
            data-testid="tree-move-to-select"
            :hint="t('tree.moveToHint')"
            :items="moveToChoices"
            :label="t('tree.newParent')"
            persistent-hint
          />
        </v-card-text>

        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="moveToOpen = false">{{ t('tree.cancel') }}</v-btn>
          <v-btn color="primary" data-testid="tree-move-to-confirm" :loading="moving" @click="confirmMoveTo">{{ t('tree.move') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="showError" color="error" timeout="5000">{{ moveError }}</v-snackbar>
  </div>
</template>

<script lang="ts" setup>
  import { computed, ref, watch } from 'vue'
  import { useI18n } from 'vue-i18n'
  import { useAuthStore } from '@/stores/auth'
  import { usePagesStore } from '@/stores/pages'
  import { useSpacesStore } from '@/stores/spaces'
  import { type DropZone, type MovePlan, planDrop, planMoveToParent, planStep, validParents, zoneFromOffset } from '@/utils/pageMove'
  import { flattenPageTree } from '@/utils/pageTree'
  import { roleAtLeast } from '@/utils/permissions'
  import { loadCollapsed, saveCollapsed, visibleRows } from '@/utils/treeCollapse'

  const props = defineProps<{ spaceId: string }>()

  defineSlots<{
    // Extension point for per-page menu entries (delete/lock/…), rendered
    // inside the row's "⋮" menu after the move actions.
    'menu-items'?: (props: { item: { id: string, title: string, locked: boolean, depth: number } }) => unknown
  }>()

  const { t } = useI18n()
  const pagesStore = usePagesStore()
  const spacesStore = useSpacesStore()
  const authStore = useAuthStore()
  const loadError = ref('')
  const moveError = ref('')
  const showError = ref(false)

  // Handles and move actions only for editor+ (Decision 24); the server
  // refuses everyone else regardless.
  const canMove = computed(() => roleAtLeast(spacesStore.spaces.find(s => s.id === props.spaceId)?.my_role, 'editor'))

  watch(() => props.spaceId, async id => {
    loadError.value = ''
    try {
      await pagesStore.fetchTree(id)
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : t('nav.loadFailed')
    }
  }, { immediate: true })

  const items = computed(() => pagesStore.treesBySpace[props.spaceId] ?? [])
  const flat = computed(() => flattenPageTree(items.value))

  // Collapsed groups persist per user and space in localStorage.
  const userKey = computed(() => authStore.profile?.email ?? authStore.displayName ?? 'anonymous')
  const collapsed = ref<Set<string>>(new Set())
  watch([() => props.spaceId, userKey], () => {
    collapsed.value = loadCollapsed(userKey.value, props.spaceId)
  }, { immediate: true })

  function toggle (id: string) {
    const next = new Set(collapsed.value)
    if (!next.delete(id)) {
      next.add(id)
    }
    collapsed.value = next
    saveCollapsed(userKey.value, props.spaceId, next)
  }

  const rows = computed(() => {
    const parents = new Set(items.value.map(item => item.parent_id))
    return visibleRows(flat.value, collapsed.value).map(item => ({ ...item, hasChildren: parents.has(item.id) }))
  })

  // --- moving ---

  const moving = ref(false)

  async function applyMove (id: string, plan: MovePlan | null): Promise<boolean> {
    if (!plan) {
      fail(t('tree.cannotMove'))
      return false
    }
    moving.value = true
    try {
      await pagesStore.movePage(props.spaceId, id, plan.parentId, plan.position)
      if (plan.parentId && collapsed.value.has(plan.parentId)) {
        toggle(plan.parentId)
      }
      return true
    } catch (error) {
      fail(error instanceof Error ? error.message : t('tree.moveFailed'))
      return false
    } finally {
      moving.value = false
    }
  }

  function fail (message: string) {
    moveError.value = message
    showError.value = true
  }

  function moveStep (id: string, direction: 'up' | 'down') {
    return applyMove(id, planStep(items.value, id, direction))
  }

  // Drag and drop (pointer users). The "⋮ → Move to…" menu is the keyboard
  // and assistive-tech equivalent.
  const dragId = ref<string | null>(null)
  const drop = ref<{ id: string, zone: DropZone } | null>(null)

  function startDrag (event: DragEvent, id: string) {
    if (!canMove.value) {
      event.preventDefault()
      return
    }
    dragId.value = id
    event.dataTransfer?.setData('text/plain', id)
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move'
    }
  }

  function overRow (event: DragEvent, id: string) {
    if (!dragId.value) {
      return
    }
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    const zone = zoneFromOffset(event.clientY - rect.top, rect.height)
    if (!planDrop(items.value, dragId.value, id, zone)) {
      drop.value = null
      return
    }
    event.preventDefault()
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move'
    }
    drop.value = { id, zone }
  }

  function dropOnRow (event: DragEvent, id: string) {
    const dragged = dragId.value
    const zone = drop.value?.id === id ? drop.value.zone : null
    endDrag()
    if (!dragged || !zone) {
      return
    }
    event.preventDefault()
    applyMove(dragged, planDrop(items.value, dragged, id, zone))
  }

  function endDrag () {
    dragId.value = null
    drop.value = null
  }

  function rowClasses (id: string) {
    return {
      'tree-dragging': dragId.value === id,
      'tree-drop-before': drop.value?.id === id && drop.value.zone === 'before',
      'tree-drop-after': drop.value?.id === id && drop.value.zone === 'after',
      'tree-drop-inside': drop.value?.id === id && drop.value.zone === 'inside',
    }
  }

  // --- Move to… dialog ---

  const moveToOpen = ref(false)
  const moveToId = ref('')
  const moveToParent = ref<string | null>(null)
  const moveToTitle = computed(() => items.value.find(item => item.id === moveToId.value)?.title ?? '')
  const moveToChoices = computed(() => {
    const allowed = new Set(validParents(items.value, moveToId.value).map(item => item.id))
    return [
      { title: t('tree.root'), value: null },
      ...flat.value
        .filter(item => allowed.has(item.id))
        .map(item => ({ title: `${' '.repeat(item.depth)}${item.title}`, value: item.id })),
    ]
  })

  function openMoveTo (id: string) {
    moveToId.value = id
    moveToParent.value = items.value.find(item => item.id === id)?.parent_id ?? null
    moveToOpen.value = true
  }

  async function confirmMoveTo () {
    if (await applyMove(moveToId.value, planMoveToParent(items.value, moveToId.value, moveToParent.value))) {
      moveToOpen.value = false
    }
  }
</script>

<style scoped>
.v-list-item :deep(.v-list-item__spacer) { width: 4px; }
.tree-handle { cursor: grab; opacity: 0.5; }
.tree-spacer { display: inline-block; width: 24px; }
.tree-dragging { opacity: 0.4; }
.tree-drop-before { box-shadow: inset 0 2px 0 rgb(var(--v-theme-primary)); }
.tree-drop-after { box-shadow: inset 0 -2px 0 rgb(var(--v-theme-primary)); }
.tree-drop-inside { background: rgba(var(--v-theme-primary), 0.14); }
</style>
