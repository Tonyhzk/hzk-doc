<script setup lang="ts">
import { useSiteStore } from '@/stores/site'
import ThemeToggle from '@/components/ThemeToggle.vue'

const { site } = useSiteStore()
</script>

<template>
  <div class="reader-page">
    <header class="site-header">
      <div class="header-inner">
        <RouterLink class="brand" to="/">{{ site.name }}</RouterLink>
        <div class="header-actions">
          <ThemeToggle />
          <RouterLink class="button button-quiet" to="/">首页</RouterLink>
        </div>
      </div>
    </header>

    <main id="main-content" class="notfound-main" tabindex="-1">
      <section class="state-panel">
        <p class="state-code">404</p>
        <h1>页面不存在</h1>
        <p>这个地址没有对应的页面，请从文档首页重新进入。</p>
        <RouterLink class="button button-primary" to="/">返回首页</RouterLink>
      </section>
    </main>

    <footer class="site-footer">
      <span>{{ site.name }}</span>
      <span v-if="site.version">版本 {{ site.version }}</span>
    </footer>
  </div>
</template>
