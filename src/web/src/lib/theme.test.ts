// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

class MediaPreference extends EventTarget {
  matches = false

  change(matches: boolean): void {
    this.matches = matches
    const event = new Event('change')
    Object.defineProperty(event, 'matches', { value: matches })
    this.dispatchEvent(event)
  }
}

let media: MediaPreference

beforeEach(() => {
  vi.resetModules()
  window.localStorage.clear()
  delete document.documentElement.dataset.theme
  document.documentElement.style.colorScheme = ''
  media = new MediaPreference()
  vi.spyOn(window, 'matchMedia').mockReturnValue(media as unknown as MediaQueryList)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('日夜模式', () => {
  it('未手动选择时随系统变化，手动选择后只在日夜之间切换', async () => {
    const { useTheme } = await import('@/composables/useTheme')
    const { isDark, toggleMode } = useTheme()
    expect(isDark.value).toBe(false)
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(window.localStorage.getItem('hzk-doc-theme')).toBeNull()

    media.change(true)
    expect(isDark.value).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('dark')
    toggleMode()
    expect(isDark.value).toBe(false)
    expect(window.localStorage.getItem('hzk-doc-theme')).toBe('light')

    media.change(false)
    media.change(true)
    expect(document.documentElement.dataset.theme).toBe('light')
    toggleMode()
    expect(isDark.value).toBe(true)
    expect(window.localStorage.getItem('hzk-doc-theme')).toBe('dark')
    toggleMode()
    expect(isDark.value).toBe(false)
  })

  it('沿用已保存的手动选择，旧的系统选项仍跟随系统', async () => {
    window.localStorage.setItem('hzk-doc-theme', 'dark')
    const first = await import('@/composables/useTheme')
    expect(first.useTheme().isDark.value).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('dark')

    vi.resetModules()
    window.localStorage.setItem('hzk-doc-theme', 'system')
    const second = await import('@/composables/useTheme')
    expect(second.useTheme().isDark.value).toBe(false)
    media.change(true)
    expect(second.useTheme().isDark.value).toBe(true)
    second.useTheme().toggleMode()
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('本地存储不可用时仍可在当前页面切换', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const { useTheme } = await import('@/composables/useTheme')
    expect(useTheme().isDark.value).toBe(false)
    useTheme().toggleMode()
    expect(useTheme().isDark.value).toBe(true)
    expect(document.documentElement.dataset.theme).toBe('dark')
  })
})
