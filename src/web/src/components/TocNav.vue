<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

import type { TocItem } from '@/api/types'
import { scrollToHeading } from '@/lib/article'

const props = defineProps<{
  items: TocItem[]
}>()

const activeId = ref('')
let observer: IntersectionObserver | null = null

watch(
  () => props.items,
  async () => {
    activeId.value = ''
    await nextTick()
    setupObserver()
  },
  { immediate: true },
)

function setupObserver(): void {
  observer?.disconnect()
  observer = null
  if (typeof IntersectionObserver === 'undefined') {
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          activeId.value = entry.target.id
        }
      }
    },
    { rootMargin: '-80px 0px -70% 0px', threshold: 0 },
  )
  for (const item of props.items) {
    const heading = document.getElementById(item.id)
    if (heading) {
      observer.observe(heading)
    }
  }
}

function go(id: string): void {
  scrollToHeading(id)
  activeId.value = id
  const url = new URL(window.location.href)
  url.hash = encodeURIComponent(id)
  window.history.replaceState(null, '', url)
}

onBeforeUnmount(() => {
  observer?.disconnect()
})
</script>

<template>
  <nav v-if="items.length > 0" class="toc" aria-label="页内目录">
    <p class="toc-title">本页目录</p>
    <ul class="toc-list">
      <li
        v-for="item in items"
        :key="item.id"
        :class="`toc-level-${item.level}`"
      >
        <a
          :href="`#${item.id}`"
          :class="{ active: activeId === item.id }"
          @click.prevent="go(item.id)"
        >
          {{ item.text }}
        </a>
      </li>
    </ul>
  </nav>
</template>
