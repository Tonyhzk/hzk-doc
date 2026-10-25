import { describe, expect, it } from 'vitest'

import {
  applyReload,
  applySaveConflict,
  applySaveSuccess,
  createEditorSnapshot,
  isEditorDirty,
} from '@/lib/editor'

describe('编辑器保存状态', () => {
  it('初始内容不视为修改', () => {
    const snapshot = createEditorSnapshot('原文', 'rev-1')
    expect(isEditorDirty(snapshot)).toBe(false)
  })

  it('修改内容后标记为未保存', () => {
    const snapshot = createEditorSnapshot('原文', 'rev-1')
    const edited = { ...snapshot, content: '新的本地内容' }
    expect(isEditorDirty(edited)).toBe(true)
  })

  it('保存成功后更新修订号并清除未保存状态', () => {
    const snapshot = { ...createEditorSnapshot('原文', 'rev-1'), content: '新内容' }
    const saved = applySaveSuccess(snapshot, 'rev-2')
    expect(saved).toEqual({
      content: '新内容',
      savedContent: '新内容',
      revision: 'rev-2',
      conflict: false,
    })
    expect(isEditorDirty(saved)).toBe(false)
  })

  it('保存期间继续编辑时仍保持未保存', () => {
    const snapshot = { ...createEditorSnapshot('原文', 'rev-1'), content: '送出的内容' }
    const saved = applySaveSuccess(snapshot, 'rev-2', '送出的内容')
    const withLaterEdit = { ...saved, content: '送出的内容加一段' }
    expect(isEditorDirty(withLaterEdit)).toBe(true)
  })

  it('409 冲突保留本地文本与旧修订号', () => {
    const snapshot = {
      ...createEditorSnapshot('服务器原文', 'rev-1'),
      content: '本地修改',
    }
    const conflict = applySaveConflict(snapshot)
    expect(conflict.content).toBe('本地修改')
    expect(conflict.savedContent).toBe('服务器原文')
    expect(conflict.revision).toBe('rev-1')
    expect(conflict.conflict).toBe(true)
    expect(isEditorDirty(conflict)).toBe(true)
  })

  it('重新加载后以服务器内容覆盖本地并解除冲突', () => {
    const snapshot = {
      ...applySaveConflict({
        ...createEditorSnapshot('旧内容', 'rev-1'),
        content: '本地修改',
      }),
    }
    const reloaded = applyReload(snapshot, '服务器新内容', 'rev-3')
    expect(reloaded).toEqual({
      content: '服务器新内容',
      savedContent: '服务器新内容',
      revision: 'rev-3',
      conflict: false,
    })
    expect(isEditorDirty(reloaded)).toBe(false)
  })
})
