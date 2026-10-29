<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'

const { isDark, description, toggleMode } = useTheme()
</script>

<template>
  <button
    type="button"
    class="button button-quiet theme-toggle"
    :title="description"
    :aria-label="description"
    @click="toggleMode"
  >
    <svg
      v-if="isDark"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.7"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2m0 16v2M2 12h2m16 0h2M4.93 4.93l1.41 1.41m11.32 11.32 1.41 1.41M19.07 4.93l-1.41 1.41M6.34 17.66l-1.41 1.41" />
    </svg>
    <svg
      v-else
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.7"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M20.3 15.5A8.5 8.5 0 0 1 8.5 3.7a8.5 8.5 0 1 0 11.8 11.8Z" />
    </svg>
  </button>
</template>
