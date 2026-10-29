// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import type { App } from 'vue'

import AdminThemeDialog from '@/components/admin/AdminThemeDialog.vue'
import type { SiteTheme } from '@/api/types'

const mocks = vi.hoisted(() => ({
  saveSiteName: vi.fn(),
  saveSiteTheme: vi.fn(),
  saveDownloadEnabled: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  saveSiteName: mocks.saveSiteName,
  saveSiteTheme: mocks.saveSiteTheme,
  saveDownloadEnabled: mocks.saveDownloadEnabled,
}))

let app: App
let root: HTMLElement
let state: { open: boolean; name: string; downloadEnabled: boolean }
const onNameSaved = vi.fn<(name: string) => void>()
const onSaved = vi.fn<(theme: SiteTheme) => void>()
const onDownloadSaved = vi.fn<(enabled: boolean) => void>()

beforeEach(async () => {
  mocks.saveSiteName.mockReset()
  mocks.saveSiteTheme.mockReset()
  mocks.saveDownloadEnabled.mockReset()
  onNameSaved.mockReset()
  onSaved.mockReset()
  onDownloadSaved.mockReset()
  state = reactive({ open: false, name: '原站点', downloadEnabled: false })
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(defineComponent({
    setup: () => () => h(AdminThemeDialog, {
      open: state.open,
      name: state.name,
      theme: 'classic',
      downloadEnabled: state.downloadEnabled,
      onNameSaved,
      onSaved,
      onDownloadSaved: (enabled: boolean) => {
        state.downloadEnabled = enabled
        onDownloadSaved(enabled)
      },
    }),
  }))
  app.mount(root)
  state.open = true
  await nextTick()
  await nextTick()
})

afterEach(() => {
  app.unmount()
  root.remove()
  vi.restoreAllMocks()
})

async function submitName(value: string): Promise<void> {
  const input = root.querySelector<HTMLInputElement>('#site-name')!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  root.querySelector<HTMLFormElement>('.site-name-form')!
    .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await Promise.resolve()
  await nextTick()
}

describe('站点设置', () => {
  it('保存下载开关并通知管理页面更新', async () => {
    const checkbox = root.querySelector<HTMLInputElement>('#site-download')!
    const form = root.querySelector<HTMLFormElement>('.site-download-form')!
    expect(checkbox.checked).toBe(false)
    mocks.saveDownloadEnabled.mockImplementation(async (enabled: boolean) => ({ downloadEnabled: enabled }))

    checkbox.checked = true
    checkbox.dispatchEvent(new Event('change', { bubbles: true }))
    expect(state.downloadEnabled).toBe(false)
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await Promise.resolve()
    await nextTick()
    expect(mocks.saveDownloadEnabled).toHaveBeenCalledWith(true)
    expect(onDownloadSaved).toHaveBeenCalledWith(true)
    expect(state.downloadEnabled).toBe(true)

    checkbox.checked = false
    checkbox.dispatchEvent(new Event('change', { bubbles: true }))
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await Promise.resolve()
    await nextTick()
    expect(mocks.saveDownloadEnabled).toHaveBeenLastCalledWith(false)
    expect(onDownloadSaved).toHaveBeenLastCalledWith(false)
    expect(state.downloadEnabled).toBe(false)
    expect(mocks.saveSiteName).not.toHaveBeenCalled()
    expect(mocks.saveSiteTheme).not.toHaveBeenCalled()
  })

  it('保存下载设置失败时不更新站点状态，重新打开恢复已保存的值', async () => {
    mocks.saveDownloadEnabled.mockRejectedValue(new Error('保存失败'))
    const checkbox = root.querySelector<HTMLInputElement>('#site-download')!
    checkbox.checked = true
    checkbox.dispatchEvent(new Event('change', { bubbles: true }))
    root.querySelector<HTMLFormElement>('.site-download-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await Promise.resolve()
    await nextTick()
    expect(onDownloadSaved).not.toHaveBeenCalled()
    expect(state.downloadEnabled).toBe(false)
    expect(root.querySelector('.site-download-form .form-error')).not.toBeNull()

    state.open = false
    await nextTick()
    state.open = true
    await nextTick()
    expect(checkbox.checked).toBe(false)
    expect(root.querySelector('.site-download-form .form-error')).toBeNull()
  })

  it('保存名称并通知管理页面更新，不修改主题', async () => {
    mocks.saveSiteName.mockResolvedValue({ name: '新站点' })
    await submitName('  新站点  ')
    expect(mocks.saveSiteName).toHaveBeenCalledWith('新站点')
    expect(onNameSaved).toHaveBeenCalledWith('新站点')
    expect(root.querySelector<HTMLInputElement>('#site-name')?.value).toBe('新站点')
    expect(mocks.saveSiteTheme).not.toHaveBeenCalled()
  })

  it('拒绝空名称和超长名称，允许 120 个 Unicode 字符', async () => {
    await submitName('   ')
    await submitName('站'.repeat(121))
    expect(mocks.saveSiteName).not.toHaveBeenCalled()
    expect(root.querySelector('.site-name-form .form-error')).not.toBeNull()
    const value = '📘'.repeat(120)
    mocks.saveSiteName.mockResolvedValue({ name: value })
    await submitName(value)
    expect(mocks.saveSiteName).toHaveBeenCalledWith(value)
  })

  it('保存主题时不修改站点名称', async () => {
    mocks.saveSiteTheme.mockResolvedValue({ theme: 'classic' })
    root.querySelector<HTMLFormElement>('.site-theme-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await Promise.resolve()
    await nextTick()
    expect(mocks.saveSiteTheme).toHaveBeenCalledWith('classic')
    expect(onSaved).toHaveBeenCalledWith('classic')
    expect(mocks.saveSiteName).not.toHaveBeenCalled()
  })
})
