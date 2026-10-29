// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import type { App } from 'vue'

import AdminThemeDialog from '@/components/admin/AdminThemeDialog.vue'
import type { SiteTheme } from '@/api/types'

const mocks = vi.hoisted(() => ({
  saveSiteName: vi.fn(),
  saveSiteTheme: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  saveSiteName: mocks.saveSiteName,
  saveSiteTheme: mocks.saveSiteTheme,
}))

let app: App
let root: HTMLElement
let state: { open: boolean; name: string }
const onNameSaved = vi.fn<(name: string) => void>()
const onSaved = vi.fn<(theme: SiteTheme) => void>()

beforeEach(async () => {
  mocks.saveSiteName.mockReset()
  mocks.saveSiteTheme.mockReset()
  onNameSaved.mockReset()
  onSaved.mockReset()
  state = reactive({ open: false, name: '原站点' })
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(defineComponent({
    setup: () => () => h(AdminThemeDialog, {
      open: state.open,
      name: state.name,
      theme: 'classic',
      onNameSaved,
      onSaved,
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
