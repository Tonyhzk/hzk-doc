import { apiRequest } from './http'
import type { DocumentData, SearchHit, SearchScope, TreeEntry } from './types'

export function fileUrl(path: string, download = false): string {
  return `/file?path=${encodeURIComponent(path)}${download ? '&download=1' : ''}`
}

export function htmlViewUrl(path: string): string {
  return `/view/html?path=${encodeURIComponent(path)}`
}

export async function fetchTree(signal?: AbortSignal): Promise<TreeEntry[]> {
  return apiRequest<TreeEntry[]>('/api/tree', { signal })
}

export async function fetchDocument(
  path: string,
  signal?: AbortSignal,
): Promise<DocumentData> {
  const document = await apiRequest<DocumentData>(
    `/api/doc?path=${encodeURIComponent(path)}`,
    { signal },
  )

  if (document.type === 'markdown') {
    if (document.rawContent === undefined) {
      document.rawContent = ''
    }
    if (document.content === undefined) {
      document.content = ''
    }
  }

  if (document.type === 'text' && document.rawContent === undefined) {
    try {
      const response = await fetch(fileUrl(path), { signal })
      if (response.ok) {
        document.rawContent = await response.text()
        document.content = document.rawContent
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw error
      }
    }
  }

  return document
}

export interface SearchOptions {
  query: string
  scope: SearchScope
  signal?: AbortSignal
}

export async function searchDocuments({
  query,
  scope,
  signal,
}: SearchOptions): Promise<SearchHit[]> {
  const parameters = new URLSearchParams({ q: query, scope })
  return apiRequest<SearchHit[]>(`/api/search?${parameters.toString()}`, { signal })
}
