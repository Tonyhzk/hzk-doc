<script setup lang="ts">
import { computed } from 'vue'

import ThemeToggle from '@/components/ThemeToggle.vue'
import { useSiteStore } from '@/stores/site'

defineEmits<{
  openNav: []
  search: []
}>()

const { site } = useSiteStore()

const shortcut = computed(() => {
  if (typeof navigator === 'undefined') {
    return 'Ctrl K'
  }
  return /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘ K' : 'Ctrl K'
})
</script>

<template>
  <header class="site-header">
    <div class="header-inner">
      <button
        type="button"
        class="button button-quiet nav-toggle"
        aria-label="打开文档目录"
        @click="$emit('openNav')"
      >
        目录
      </button>
      <RouterLink class="brand" to="/" :title="site.name">
        {{ site.name }}
      </RouterLink>
      <div class="header-actions">
        <button
          type="button"
          class="search-trigger"
          aria-label="搜索文档"
          @click="$emit('search')"
        >
          <span>搜索</span>
          <kbd class="shortcut">{{ shortcut }}</kbd>
        </button>
        <ThemeToggle />
        <RouterLink class="button button-quiet header-link" to="/admin">
          管理
        </RouterLink>
      </div>
    </div>
  </header>
</template>
