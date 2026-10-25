import { describe, expect, it } from 'vitest'

import { pageToc } from '@/lib/article'

describe('pageToc', () => {
  const items = [
    { id: 'title', text: '开始使用', level: 1 },
    { id: 'read', text: '阅读文档', level: 2 },
    { id: 'deep', text: '细节', level: 4 },
    { id: 'code', text: '代码', level: 3 },
  ]

  it('移除与页面标题重复的一级标题', () => {
    expect(pageToc(items, '开始使用')).toEqual([
      { id: 'read', text: '阅读文档', level: 2 },
      { id: 'code', text: '代码', level: 3 },
    ])
  })

  it('标题不一致时保留一级标题', () => {
    expect(pageToc(items, 'README')).toEqual([
      { id: 'title', text: '开始使用', level: 1 },
      { id: 'read', text: '阅读文档', level: 2 },
      { id: 'code', text: '代码', level: 3 },
    ])
  })

  it('只保留一到三级标题', () => {
    expect(pageToc(items, 'README').map((item) => item.level)).toEqual([
      1, 2, 3,
    ])
  })

  it('忽略标题首尾与连续空白差异', () => {
    expect(pageToc(items, '  开始   使用 ')).toEqual([
      { id: 'read', text: '阅读文档', level: 2 },
      { id: 'code', text: '代码', level: 3 },
    ])
  })
})
