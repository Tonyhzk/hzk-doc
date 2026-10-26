import { describe, expect, it } from 'vitest'

import type { TreeEntry, TreeFile } from '@/api/types'
import {
  documentNeighbors,
  findFileByPath,
  flattenFiles,
  folderAncestors,
} from '@/lib/tree'

function file(id: string, path: string): TreeFile {
  return {
    type: 'file',
    id,
    name: path.slice(path.lastIndexOf('/') + 1),
    path,
    displayName: '',
    fileType: 'markdown',
    fileTypeCategory: 'markdown',
  }
}

const entries: TreeEntry[] = [
  file('id-intro', 'intro.md'),
  {
    type: 'folder',
    name: '指南',
    path: '指南',
    children: [
      file('id-start', '指南/start.md'),
      {
        type: 'folder',
        name: '进阶',
        path: '指南/进阶',
        children: [file('id-deep', '指南/进阶/deep.md')],
      },
    ],
  },
]

describe('findFileByPath', () => {
  it('按路径找到文件，供 Markdown 相对链接换算 ID', () => {
    expect(findFileByPath(entries, '指南/start.md')?.id).toBe('id-start')
  })

  it('未知或空路径返回 null', () => {
    expect(findFileByPath(entries, '指南/missing.md')).toBeNull()
    expect(findFileByPath(entries, '')).toBeNull()
  })
})

describe('documentNeighbors', () => {
  it('按 ID 计算上一篇与下一篇', () => {
    const neighbors = documentNeighbors(entries, 'id-start')
    expect(neighbors.previous?.id).toBe('id-intro')
    expect(neighbors.next?.id).toBe('id-deep')
  })

  it('首尾文档只有一侧邻接', () => {
    expect(documentNeighbors(entries, 'id-intro').previous).toBeNull()
    expect(documentNeighbors(entries, 'id-deep').next).toBeNull()
  })

  it('未知 ID 返回空邻接', () => {
    expect(documentNeighbors(entries, 'missing')).toEqual({
      previous: null,
      next: null,
    })
  })
})

describe('folderAncestors', () => {
  it('按 ID 返回当前文件所在目录链', () => {
    expect(folderAncestors(entries, 'id-deep')).toEqual(['指南', '指南/进阶'])
    expect(folderAncestors(entries, 'id-intro')).toEqual([])
  })

  it('未知 ID 返回空链', () => {
    expect(folderAncestors(entries, 'missing')).toEqual([])
  })
})

describe('flattenFiles', () => {
  it('按目录顺序展开全部文件', () => {
    expect(flattenFiles(entries).map((entry) => entry.id)).toEqual([
      'id-intro',
      'id-start',
      'id-deep',
    ])
  })
})
