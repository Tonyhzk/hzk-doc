<script setup lang="ts">
import { ref, watch } from 'vue'

import type { TreeEntry } from '@/api/types'
import TreeBranch from '@/components/TreeBranch.vue'
import { folderAncestors } from '@/lib/tree'

const props = defineProps<{
  entries: TreeEntry[]
  currentPath: string
}>()

const emit = defineEmits<{
  select: [path: string]
}>()

const expanded = ref(new Set<string>())

watch(
  () => props.currentPath,
  (path) => {
    if (!path) {
      return
    }
    const next = new Set(expanded.value)
    for (const folder of folderAncestors(props.entries, path)) {
      next.add(folder)
    }
    expanded.value = next
  },
  { immediate: true },
)

watch(
  () => props.entries,
  () => {
    if (!props.currentPath) {
      return
    }
    const next = new Set(expanded.value)
    for (const folder of folderAncestors(props.entries, props.currentPath)) {
      next.add(folder)
    }
    expanded.value = next
  },
)

function toggle(path: string): void {
  const next = new Set(expanded.value)
  if (next.has(path)) {
    next.delete(path)
  } else {
    next.add(path)
  }
  expanded.value = next
}
</script>

<template>
  <nav class="doc-tree" aria-label="文档目录">
    <p v-if="entries.length === 0" class="tree-empty">文档目录为空。</p>
    <ul v-else class="tree-list">
      <TreeBranch
        v-for="entry in entries"
        :key="entry.path"
        :node="entry"
        :depth="0"
        :expanded="expanded"
        :current-path="currentPath"
        @toggle="toggle"
        @select="emit('select', $event)"
      />
    </ul>
  </nav>
</template>
