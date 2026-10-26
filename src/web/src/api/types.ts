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
  id: string
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
  id: string
  path: string
  title: string
  content?: string
  rawContent?: string
  url?: string
  toc?: TocItem[]
  size?: number
  modifiedAt?: string
}

export type SearchScope = 'title' | 'full'

export interface SearchHit {
  id: string
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
  id?: string
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
  id?: string
  path: string
  content: string
  revision: string
}

export interface AdminDocumentResult {
  id?: string
  path: string
  revision: string
}

export interface AdminRenameResult {
  id: string
  path: string
}

export interface AdminOrderResult {
  directory: string
  names: string[]
}

export interface AdminUploadResult {
  id?: string
  path: string
  size: number
}

export interface TrashItem {
  path: string
  type: 'folder' | 'file'
  size: number
}

export interface TrashEntry {
  id: string
  deletedAt: string
  items: TrashItem[]
  size: number
}

export interface TrashData {
  entries: TrashEntry[]
  size: number
}

export interface TrashActionResult {
  count: number
}

export interface PreviewResult {
  content: string
}

export interface ApiEnvelope<T> {
  success: boolean
  data?: T
  error?: string
}
