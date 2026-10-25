<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

import type { TreeEntry } from '@/api/types'
import TreeNav from '@/components/TreeNav.vue'

const props = defineProps<{
  open: boolean
  entries: TreeEntry[]
  currentPath: string
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  select: [path: string]
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      closeButton.value?.focus()
    } else if (element.open) {
      element.close()
    }
  },
  { immediate: true },
)

function close(): void {
  emit('update:open', false)
}

function onSelect(path: string): void {
  emit('select', path)
  close()
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    close()
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="tree-drawer"
    aria-label="文档目录"
    @close="emit('update:open', false)"
    @click="onBackdrop"
  >
    <div class="drawer-head">
      <h2>文档目录</h2>
      <button
        ref="closeButton"
        type="button"
        class="button button-quiet"
        @click="close"
      >
        关闭
      </button>
    </div>
    <div class="drawer-body">
      <TreeNav
        :entries="entries"
        :current-path="currentPath"
        @select="onSelect"
      />
    </div>
  </dialog>
</template>
