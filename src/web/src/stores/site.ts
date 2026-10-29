import { reactive, ref } from 'vue'

import { fetchSite } from '@/api/site'
import type { SiteInfo, SiteTheme } from '@/api/types'

export type SiteState = 'idle' | 'loading' | 'ready' | 'error'

const site = reactive<SiteInfo>({
  name: '文档',
  version: '',
  count: 0,
  adminEnabled: false,
  theme: 'classic',
})

const state = ref<SiteState>('idle')
let pending: Promise<boolean> | null = null

async function loadSite(): Promise<boolean> {
  if (state.value === 'ready') {
    return true
  }
  if (pending) {
    return pending
  }

  state.value = 'loading'
  pending = (async () => {
    try {
      const info = await fetchSite()
      Object.assign(site, info)
      applySiteTheme(info.theme)
      state.value = 'ready'
      return true
    } catch {
      state.value = 'error'
      return false
    } finally {
      pending = null
    }
  })()
  return pending
}

function applySiteTheme(theme: SiteTheme): void {
  site.theme = theme
  if (typeof document !== 'undefined') {
    document.documentElement.dataset.palette = theme
  }
}

export function useSiteStore() {
  return { site, loadSite, applySiteTheme }
}
