// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import type { App } from 'vue'

import type { AdminEntry } from '@/api/types'
import AdminBrowser from '@/components/admin/AdminBrowser.vue'

const mocks = vi.hoisted(() => ({
  fetchEntries: vi.fn(),
  saveOrder: vi.fn(),
  deleteEntry: vi.fn(),
}))

vi.mock('@/api/admin', () => mocks)
vi.mock('@/components/admin/AdminCreateDialog.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/admin/AdminRenameDialog.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/admin/AdminUploadDialog.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/ConfirmDialog.vue', () => ({ default: { render: () => null } }))

const initialEntries: AdminEntry[] = [
  { name: 'a.md', path: 'a.md', type: 'file', kind: 'markdown', size: 1, modifiedAt: '', editable: true, id: 'a' },
  { name: 'folder', path: 'folder', type: 'folder', kind: 'folder', size: 0, modifiedAt: '', editable: false },
  { name: 'c.md', path: 'c.md', type: 'file', kind: 'markdown', size: 1, modifiedAt: '', editable: true, id: 'c' },
  { name: 'd.md', path: 'd.md', type: 'file', kind: 'markdown', size: 1, modifiedAt: '', editable: true, id: 'd' },
]

let app: App
let root: HTMLElement

async function flush(): Promise<void> {
  await Promise.resolve()
  await nextTick()
  await Promise.resolve()
  await nextTick()
}

beforeEach(async () => {
  mocks.fetchEntries.mockReset()
  mocks.saveOrder.mockReset()
  mocks.deleteEntry.mockReset()
  mocks.fetchEntries.mockResolvedValue({ directory: '', entries: initialEntries.slice() })
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(AdminBrowser)
  app.mount(root)
  await flush()
  root.querySelector<HTMLButtonElement>('.order-start')!
    .dispatchEvent(new Event('click', { bubbles: true }))
  await nextTick()
})

afterEach(() => {
  app.unmount()
  root.remove()
  vi.restoreAllMocks()
})

function rows(): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>('.entry-row'))
}

function paths(): string[] {
  return rows().map((row) => row.dataset.orderPath!)
}

function pointer(handle: HTMLElement, type: string, y: number, pointerType = 'mouse'): void {
  handle.dispatchEvent(new PointerEvent(type, {
    bubbles: true,
    cancelable: true,
    pointerId: 7,
    pointerType,
    button: 0,
    clientX: 150,
    clientY: y,
  }))
}

function targetRow(index: number): HTMLElement {
  const target = rows()[index]!
  vi.spyOn(target, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 100, 300, 40))
  vi.spyOn(document, 'elementFromPoint').mockReturnValue(target)
  return target
}

async function drag(source: number, target: number, after: boolean, pointerType = 'mouse'): Promise<void> {
  targetRow(target)
  const handle = rows()[source]!.querySelector<HTMLElement>('.order-handle')!
  const y = after ? 130 : 110
  pointer(handle, 'pointerdown', y, pointerType)
  pointer(handle, 'pointermove', y, pointerType)
  await nextTick()
  pointer(handle, 'pointerup', y, pointerType)
  await nextTick()
}

describe('目录拖拽排序', () => {
  it('为每个条目提供排序手柄，不显示普通操作', () => {
    expect(rows()).toHaveLength(initialEntries.length)
    expect(root.querySelectorAll('.order-handle')).toHaveLength(initialEntries.length)
    expect(root.querySelectorAll('.entry-actions')).toHaveLength(0)
  })

  it('将条目拖到目标之后', async () => {
    await drag(0, 2, true)
    expect(paths()).toEqual(['folder', 'c.md', 'a.md', 'd.md'])
    expect(root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')?.disabled).toBe(false)
  })

  it('将条目拖到目标之前', async () => {
    await drag(3, 1, false)
    expect(paths()).toEqual(['a.md', 'd.md', 'folder', 'c.md'])
  })

  it('支持触摸拖动至末尾', async () => {
    await drag(0, 3, true, 'touch')
    expect(paths()).toEqual(['folder', 'c.md', 'd.md', 'a.md'])
  })

  it('取消拖动时保留原顺序', async () => {
    targetRow(2)
    const handle = rows()[0]!.querySelector<HTMLElement>('.order-handle')!
    pointer(handle, 'pointerdown', 130)
    pointer(handle, 'pointermove', 130)
    await nextTick()
    expect(rows()[2]!.classList.contains('drop-after')).toBe(true)
    pointer(handle, 'pointercancel', 130)
    await nextTick()
    expect(paths()).toEqual(initialEntries.map((entry) => entry.path))
    expect(root.querySelector('.is-dragging, .drop-before, .drop-after')).toBeNull()
    expect(root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')?.disabled).toBe(true)
  })

  it('恢复初始顺序后无需保存', async () => {
    await drag(0, 2, true)
    await drag(2, 0, false)
    expect(paths()).toEqual(initialEntries.map((entry) => entry.path))
    expect(root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')?.disabled).toBe(true)
  })

  it('保存拖动后的完整顺序并重新加载', async () => {
    mocks.saveOrder.mockResolvedValue(undefined)
    await drag(0, 2, true)
    mocks.fetchEntries.mockResolvedValueOnce({
      directory: '',
      entries: [initialEntries[1], initialEntries[2], initialEntries[0], initialEntries[3]],
    })
    root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')!.click()
    await flush()
    expect(mocks.saveOrder).toHaveBeenCalledOnce()
    expect(mocks.saveOrder).toHaveBeenCalledWith('', ['folder', 'c.md', 'a.md', 'd.md'])
    expect(mocks.fetchEntries).toHaveBeenCalledTimes(2)
    expect(root.querySelector('.order-handle')).toBeNull()
    expect(rows().map((row) => row.querySelector('.entry-name')?.textContent?.trim()))
      .toEqual(['folder', 'c.md', 'a.md', 'd.md'])
  })

  it('保存失败时保留调整结果并可重试', async () => {
    mocks.saveOrder.mockRejectedValueOnce(new Error('保存失败')).mockResolvedValue(undefined)
    await drag(0, 2, true)
    root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')!.click()
    await flush()
    expect(paths()).toEqual(['folder', 'c.md', 'a.md', 'd.md'])
    expect(root.querySelector('.banner-error')).not.toBeNull()
    expect(root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')?.disabled).toBe(false)
    mocks.fetchEntries.mockResolvedValueOnce({
      directory: '',
      entries: [initialEntries[1], initialEntries[2], initialEntries[0], initialEntries[3]],
    })
    root.querySelector<HTMLButtonElement>('.admin-toolbar .button-primary')!.click()
    await flush()
    expect(mocks.saveOrder).toHaveBeenCalledTimes(2)
    expect(mocks.saveOrder).toHaveBeenLastCalledWith('', ['folder', 'c.md', 'a.md', 'd.md'])
    expect(root.querySelector('.order-handle')).toBeNull()
    expect(rows().map((row) => row.querySelector('.entry-name')?.textContent?.trim()))
      .toEqual(['folder', 'c.md', 'a.md', 'd.md'])
  })

  it('允许通过键盘调整顺序并保留焦点', async () => {
    const handle = rows()[0]!.querySelector<HTMLButtonElement>('.order-handle')!
    handle.focus()
    handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }))
    await flush()
    expect(paths()).toEqual(['folder', 'a.md', 'c.md', 'd.md'])
    expect(document.activeElement).toBe(handle)
  })
})
