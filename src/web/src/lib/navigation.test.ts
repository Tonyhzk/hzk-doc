import { describe, expect, it } from 'vitest'

import {
  decodeFragment,
  directoryOf,
  joinPath,
  parseDocParam,
  readingHref,
  resolveMarkdownHref,
  resolveViewerHref,
} from '@/lib/navigation'

describe('parseDocParam', () => {
  it('读取普通路径并去掉前导斜杠', () => {
    expect(parseDocParam('/指南/开始.md')).toBe('指南/开始.md')
  })

  it('处理数组形式并忽略空值', () => {
    expect(parseDocParam(['指南/开始.md', '其他.md'])).toBe('指南/开始.md')
    expect(parseDocParam(null)).toBe('')
    expect(parseDocParam(undefined)).toBe('')
    expect(parseDocParam(42)).toBe('')
  })

  it('去掉首尾空白', () => {
    expect(parseDocParam('  指南/开始.md  ')).toBe('指南/开始.md')
  })
})

describe('readingHref', () => {
  it('使用 URLSearchParams 编码文档路径', () => {
    expect(readingHref('指南/开始.md')).toBe(
      '/?doc=%E6%8C%87%E5%8D%97%2F%E5%BC%80%E5%A7%8B.md',
    )
  })

  it('编码标题片段', () => {
    expect(readingHref('开始.md', '第一 节')).toBe(
      '/?doc=%E5%BC%80%E5%A7%8B.md#%E7%AC%AC%E4%B8%80%20%E8%8A%82',
    )
  })
})

describe('resolveMarkdownHref', () => {
  it('解析同目录 Markdown 链接和片段', () => {
    expect(resolveMarkdownHref('next.md#小节', '指南/start.md')).toEqual({
      path: '指南/next.md',
      hash: '小节',
    })
  })

  it('解析上级目录链接', () => {
    expect(resolveMarkdownHref('../other.md', '指南/start.md')).toEqual({
      path: 'other.md',
      hash: '',
    })
  })

  it('解析根路径链接并忽略查询参数', () => {
    expect(resolveMarkdownHref('/root.md?x=1#a', '指南/start.md')).toEqual({
      path: 'root.md',
      hash: 'a',
    })
  })

  it('解码百分号编码的路径', () => {
    expect(resolveMarkdownHref('%E4%B8%AD%E6%96%87.md', 'start.md')).toEqual({
      path: '中文.md',
      hash: '',
    })
  })

  it('忽略外链、非 Markdown 与纯片段', () => {
    expect(
      resolveMarkdownHref('https://example.com/a.md', 'start.md'),
    ).toBeNull()
    expect(resolveMarkdownHref('#section', 'start.md')).toBeNull()
    expect(resolveMarkdownHref('image.png', 'start.md')).toBeNull()
    expect(resolveMarkdownHref('', 'start.md')).toBeNull()
  })
})

describe('resolveViewerHref', () => {
  const base = 'http://localhost:8080/'

  it('解析同站阅读链接与标题片段', () => {
    expect(
      resolveViewerHref(
        '/?doc=guides%2Fother.md#%E6%A0%87%E9%A2%98',
        base,
      ),
    ).toEqual({
      path: 'guides/other.md',
      hash: '标题',
    })
  })

  it('忽略其他站点、其他路径和缺少 doc 的地址', () => {
    expect(
      resolveViewerHref('https://example.com/?doc=a.md', base),
    ).toBeNull()
    expect(resolveViewerHref('/view/html?path=a.html', base)).toBeNull()
    expect(resolveViewerHref('/?q=a', base)).toBeNull()
    expect(resolveViewerHref('#a', base)).toBeNull()
  })
})

describe('路径与片段工具', () => {
  it('计算目录与拼接路径', () => {
    expect(directoryOf('a/b/c.md')).toBe('a/b')
    expect(directoryOf('c.md')).toBe('')
    expect(joinPath('', 'c.md')).toBe('c.md')
    expect(joinPath('a/b', 'c.md')).toBe('a/b/c.md')
  })

  it('解码标题片段并容忍错误编码', () => {
    expect(decodeFragment('#%E6%A0%87%E9%A2%98')).toBe('标题')
    expect(decodeFragment('title')).toBe('title')
    expect(decodeFragment('#%E0%A4%A')).toBe('%E0%A4%A')
    expect(decodeFragment('')).toBe('')
  })
})
