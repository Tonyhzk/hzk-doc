import { describe, expect, it } from 'vitest'

import { canMove, moveItemBy, orderNames } from '@/lib/order'

describe('moveItemBy', () => {
  const items = ['a', 'b', 'c']

  it('向上移动一项', () => {
    expect(moveItemBy(items, 1, -1)).toEqual(['b', 'a', 'c'])
  })

  it('向下移动一项', () => {
    expect(moveItemBy(items, 0, 1)).toEqual(['b', 'a', 'c'])
  })

  it('到达边界时保持顺序', () => {
    expect(moveItemBy(items, 0, -1)).toEqual(['a', 'b', 'c'])
    expect(moveItemBy(items, 2, 1)).toEqual(['a', 'b', 'c'])
  })

  it('不修改原数组', () => {
    const original = items.slice()
    const moved = moveItemBy(items, 0, 1)
    expect(items).toEqual(original)
    expect(moved).not.toBe(items)
  })

  it('索引异常时返回副本', () => {
    expect(moveItemBy(items, -1, 1)).toEqual(['a', 'b', 'c'])
    expect(moveItemBy(items, 3, -1)).toEqual(['a', 'b', 'c'])
    const copy = moveItemBy(items, 3, -1)
    expect(copy).not.toBe(items)
  })
})

describe('canMove', () => {
  it('判断上下移动是否有效', () => {
    expect(canMove(0, 3, -1)).toBe(false)
    expect(canMove(0, 3, 1)).toBe(true)
    expect(canMove(2, 3, 1)).toBe(false)
    expect(canMove(2, 3, -1)).toBe(true)
    expect(canMove(0, 1, 0)).toBe(false)
  })
})

describe('orderNames', () => {
  it('按当前顺序提取全部名称', () => {
    expect(orderNames([{ name: 'b.md' }, { name: 'a' }, { name: 'x.zip' }])).toEqual([
      'b.md',
      'a',
      'x.zip',
    ])
  })
})
