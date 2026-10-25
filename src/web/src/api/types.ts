export interface TreeFolder {
  type: 'folder'
  name: string
  path: string
  children: TreeEntry[]
}

export interface TreeFile {
  type: 'file'
  name: string
  path: string
  displayName: string
  fileType: string
  fileTypeCategory: string
  id?: string
}

export type TreeEntry = TreeFolder | TreeFile

export interface SiteInfo {
  name: string
  version: string
  count: number
  adminEnabled: boolean
}

export interface TocItem {
  id: string
  text: string
  level: number
}

export interface DocumentData {
  type: string
  path: string
  title: string
  content?: string
  rawContent?: string
  url?: string
  toc?: TocItem[]
  size?: number
  modifiedAt?: string
  id?: string
}

export type SearchScope = 'title' | 'full'

export interface SearchHit {
  path: string
  title: string
  directory: string
  extension: string
  snippet: string
}

export interface AdminSession {
  enabled: boolean
  authenticated: boolean
  csrfToken?: string
}

export interface AdminEntry {
  name: string
  path: string
  type: 'folder' | 'file'
  kind: string
  size: number
  modifiedAt: string
  editable: boolean
}

export interface AdminEntries {
  directory: string
  entries: AdminEntry[]
}

export interface AdminSource {
  path: string
  content: string
  revision: string
}

export interface AdminDocumentResult {
  path: string
  revision: string
}

export interface AdminOrderResult {
  directory: string
  names: string[]
}

export interface AdminUploadResult {
  path: string
  size: number
}

export interface PreviewResult {
  content: string
}

export interface ApiEnvelope<T> {
  success: boolean
  data?: T
  error?: string
}
