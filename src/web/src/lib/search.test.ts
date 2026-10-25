import { describe, expect, it } from 'vitest'

import { parseSnippet } from '@/lib/search'

describe('parseSnippet', () => {
  it('拆分单个命中片段', () => {
    expect(parseSnippet('前面[[关键词]]后面')).toEqual([
      { text: '前面', match: false },
      { text: '关键词', match: true },
      { text: '后面', match: false },
    ])
  })

  it('拆分多个命中片段', () => {
    expect(parseSnippet('[[甲]]中间[[乙]]')).toEqual([
      { text: '甲', match: true },
      { text: '中间', match: false },
      { text: '乙', match: true },
    ])
  })

  it('没有命中标记时保持原文', () => {
    expect(parseSnippet('纯文本')).toEqual([
      { text: '纯文本', match: false },
    ])
  })

  it('未闭合的标记按普通文本处理', () => {
    expect(parseSnippet('未闭合 [[关键词')).toEqual([
      { text: '未闭合 [[关键词', match: false },
    ])
  })

  it('空字符串返回空数组', () => {
    expect(parseSnippet('')).toEqual([])
  })

  it('类似 HTML 的内容只作为纯文本片段', () => {
    expect(parseSnippet('<b>[[命中]]</b>')).toEqual([
      { text: '<b>', match: false },
      { text: '命中', match: true },
      { text: '</b>', match: false },
    ])
  })
})
