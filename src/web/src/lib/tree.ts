import type { TreeEntry, TreeFile, TreeFolder } from '@/api/types'

export function flattenFiles(entries: readonly TreeEntry[]): TreeFile[] {
  const files: TreeFile[] = []
  for (const entry of entries) {
    if (entry.type === 'file') {
      files.push(entry)
      continue
    }
    files.push(...flattenFiles(entry.children))
  }
  return files
}

export function countDocuments(entries: readonly TreeEntry[]): number {
  return flattenFiles(entries).length
}

export function countFolderDocuments(folder: TreeFolder): number {
  return flattenFiles(folder.children).length
}

export function folderAncestors(
  entries: readonly TreeEntry[],
  id: string,
): string[] {
  function walk(nodes: readonly TreeEntry[], trail: string[]): string[] | null {
    for (const entry of nodes) {
      if (entry.type === 'file') {
        if (entry.id === id) {
          return trail
        }
        continue
      }
      const found = walk(entry.children, [...trail, entry.path])
      if (found) {
        return found
      }
    }
    return null
  }
  return walk(entries, []) ?? []
}

export function findFileByPath(
  entries: readonly TreeEntry[],
  path: string,
): TreeFile | null {
  if (!path) {
    return null
  }
  return flattenFiles(entries).find((entry) => entry.path === path) ?? null
}

export interface DocumentNeighbors {
  previous: TreeFile | null
  next: TreeFile | null
}

export function documentNeighbors(
  entries: readonly TreeEntry[],
  id: string,
): DocumentNeighbors {
  const files = flattenFiles(entries)
  const index = files.findIndex((entry) => entry.id === id)
  if (index === -1) {
    return { previous: null, next: null }
  }
  return {
    previous: index > 0 ? (files[index - 1] ?? null) : null,
    next: index < files.length - 1 ? (files[index + 1] ?? null) : null,
  }
}

export interface PathSegment {
  name: string
  path: string
}

export function pathTrail(path: string): PathSegment[] {
  if (!path) {
    return []
  }
  const segments: PathSegment[] = []
  let current = ''
  for (const name of path.split('/')) {
    if (!name) {
      continue
    }
    current = current ? `${current}/${name}` : name
    segments.push({ name, path: current })
  }
  return segments
}

export function folderTrail(path: string): PathSegment[] {
  const segments = pathTrail(path)
  return segments.length > 0 ? segments.slice(0, -1) : []
}
