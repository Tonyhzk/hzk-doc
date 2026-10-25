<script setup lang="ts">
import { useTheme } from '@/composables/useTheme'

const { label, description, cycleMode } = useTheme()
</script>

<template>
  <button
    type="button"
    class="button button-quiet theme-toggle"
    :title="description"
    :aria-label="description"
    @click="cycleMode"
  >
    {{ label }}
  </button>
</template>
