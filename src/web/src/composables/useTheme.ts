import { computed, ref } from 'vue'

type ThemeMode = 'light' | 'dark'

const STORAGE_KEY = 'hzk-doc-theme'
const systemPreference =
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)')
    : null
const preference = ref<ThemeMode | null>(readStoredMode())
const systemDark = ref(systemPreference?.matches ?? false)
const isDark = computed(() =>
  preference.value ? preference.value === 'dark' : systemDark.value,
)
const description = computed(() =>
  isDark.value ? '切换到浅色' : '切换到深色',
)

function readStoredMode(): ThemeMode | null {
  if (typeof window === 'undefined') {
    return null
  }
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    return stored === 'light' || stored === 'dark' ? stored : null
  } catch {
    return null
  }
}

function applyTheme(): void {
  if (typeof document === 'undefined') {
    return
  }
  const mode = isDark.value ? 'dark' : 'light'
  document.documentElement.dataset.theme = mode
  document.documentElement.style.colorScheme = mode
}

function toggleMode(): void {
  const mode = isDark.value ? 'light' : 'dark'
  preference.value = mode
  try {
    window.localStorage.setItem(STORAGE_KEY, mode)
  } catch {
    // 无法保存时只在当前会话生效
  }
  applyTheme()
}

systemPreference?.addEventListener('change', (event) => {
  systemDark.value = event.matches
  if (!preference.value) {
    applyTheme()
  }
})
applyTheme()

export function useTheme() {
  return { isDark, description, toggleMode }
}
