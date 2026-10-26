<script setup lang="ts">
import { computed } from 'vue'

import type { TreeEntry } from '@/api/types'
import { fileMark } from '@/lib/format'
import { readingHref } from '@/lib/navigation'

const props = defineProps<{
  node: TreeEntry
  depth: number
  expanded: Set<string>
  currentId: string
}>()

const emit = defineEmits<{
  toggle: [path: string]
  select: [id: string]
}>()

const rowPadding = computed(() => `${8 + props.depth * 14}px`)

function onClick(event: MouseEvent): void {
  if (
    props.node.type !== 'file' ||
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  ) {
    return
  }
  event.preventDefault()
  emit('select', props.node.id)
}
</script>

<template>
  <li v-if="node.type === 'folder'" class="tree-item">
    <button
      type="button"
      class="tree-row tree-folder"
      :style="{ paddingLeft: rowPadding }"
      :aria-expanded="expanded.has(node.path)"
      @click="$emit('toggle', node.path)"
    >
      <span
        class="tree-chevron"
        :class="{ 'is-open': expanded.has(node.path) }"
        aria-hidden="true"
      ></span>
      <span class="tree-label">{{ node.name }}</span>
    </button>
    <ul v-if="expanded.has(node.path)" class="tree-children">
      <TreeBranch
        v-for="child in node.children"
        :key="child.path"
        :node="child"
        :depth="depth + 1"
        :expanded="expanded"
        :current-id="currentId"
        @toggle="$emit('toggle', $event)"
        @select="$emit('select', $event)"
      />
    </ul>
  </li>
  <li v-else class="tree-item">
    <a
      class="tree-row tree-file"
      :class="{ active: node.id === currentId }"
      :href="readingHref(node.id)"
      :style="{ paddingLeft: rowPadding }"
      :aria-current="node.id === currentId ? 'page' : undefined"
      @click="onClick"
    >
      <span class="file-mark" aria-hidden="true">
        {{ fileMark(node.fileTypeCategory, node.path) }}
      </span>
      <span class="tree-label">{{ node.displayName || node.name }}</span>
    </a>
  </li>
</template>
