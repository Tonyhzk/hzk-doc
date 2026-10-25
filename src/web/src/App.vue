<script setup lang="ts">
import { onMounted } from 'vue'

import { useSiteStore } from '@/stores/site'

const { loadSite } = useSiteStore()

onMounted(() => {
  void loadSite()
})
</script>

<template>
  <a class="skip-link" href="#main-content">跳到正文</a>
  <RouterView />
</template>
