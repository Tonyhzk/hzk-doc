import { computed, ref } from 'vue'

export type ThemeMode = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'hzk-doc-theme'
const mode = ref<ThemeMode>(readStoredMode())

const label = computed(() => {
  switch (mode.value) {
    case 'light':
      return '浅色'
    case 'dark':
      return '深色'
    default:
      return '系统'
  }
})

const description = computed(() => {
  switch (mode.value) {
    case 'light':
      return '主题：浅色，点击切换'
    case 'dark':
      return '主题：深色，点击切换'
    default:
      return '主题：跟随系统，点击切换'
  }
})

function readStoredMode(): ThemeMode {
  if (typeof window === 'undefined') {
    return 'system'
  }
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark' || stored === 'system') {
      return stored
    }
  } catch {
    // 本地存储不可用时使用系统主题
  }
  return 'system'
}

function applyTheme(): void {
  if (typeof document === 'undefined') {
    return
  }
  const root = document.documentElement
  if (mode.value === 'system') {
    delete root.dataset.theme
    root.style.colorScheme = 'light dark'
    return
  }
  root.dataset.theme = mode.value
  root.style.colorScheme = mode.value
}

function setMode(value: ThemeMode): void {
  mode.value = value
  try {
    window.localStorage.setItem(STORAGE_KEY, value)
  } catch {
    // 无法保存时只在当前会话生效
  }
  applyTheme()
}

function cycleMode(): void {
  const order: ThemeMode[] = ['system', 'light', 'dark']
  const index = order.indexOf(mode.value)
  setMode(order[(index + 1) % order.length] ?? 'system')
}

applyTheme()

export function useTheme() {
  return {
    label,
    description,
    cycleMode,
  }
}
