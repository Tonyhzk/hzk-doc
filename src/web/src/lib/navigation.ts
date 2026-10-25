export interface ResolvedDocLink {
  path: string
  hash: string
}

export function parseDocParam(value: unknown): string {
  const raw = Array.isArray(value) ? value[0] : value
  if (typeof raw !== 'string') {
    return ''
  }
  return raw.trim().replace(/^\/+/, '')
}

export function readingHref(path: string, heading = ''): string {
  const query = new URLSearchParams({ doc: path }).toString()
  const fragment = heading ? `#${encodeURIComponent(heading)}` : ''
  return `/?${query}${fragment}`
}

export function directoryOf(path: string): string {
  const index = path.lastIndexOf('/')
  return index === -1 ? '' : path.slice(0, index)
}

export function joinPath(directory: string, name: string): string {
  return directory ? `${directory}/${name}` : name
}

export function decodeFragment(hash: string): string {
  const value = hash.startsWith('#') ? hash.slice(1) : hash
  if (!value) {
    return ''
  }
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function normalizePath(value: string): string {
  const parts: string[] = []
  for (const part of value.split('/')) {
    if (!part || part === '.') {
      continue
    }
    if (part === '..') {
      parts.pop()
      continue
    }
    parts.push(part)
  }
  return parts.join('/')
}

function markdownExtension(path: string): boolean {
  return /\.(md|markdown|mark)$/i.test(path)
}

export function resolveMarkdownHref(
  href: string,
  currentPath: string,
): ResolvedDocLink | null {
  const raw = href.trim()
  if (!raw || raw.startsWith('#') || raw.startsWith('//')) {
    return null
  }
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(raw)) {
    return null
  }

  const hashIndex = raw.indexOf('#')
  const beforeHash = hashIndex === -1 ? raw : raw.slice(0, hashIndex)
  const rawHash = hashIndex === -1 ? '' : raw.slice(hashIndex + 1)
  const queryIndex = beforeHash.indexOf('?')
  const rawPath = queryIndex === -1 ? beforeHash : beforeHash.slice(0, queryIndex)
  if (!rawPath) {
    return null
  }

  const decodedPath = safeDecode(rawPath)
  if (!markdownExtension(decodedPath)) {
    return null
  }

  const relative = decodedPath.startsWith('/')
    ? decodedPath.slice(1)
    : normalizePath(`${directoryOf(currentPath)}/${decodedPath}`)
  if (!relative) {
    return null
  }

  return {
    path: relative,
    hash: safeDecode(rawHash),
  }
}

export function resolveViewerHref(
  href: string,
  baseUrl: string,
): ResolvedDocLink | null {
  if (!href || href.startsWith('#')) {
    return null
  }
  let base: URL
  let target: URL
  try {
    base = new URL(baseUrl)
    target = new URL(href, base)
  } catch {
    return null
  }
  if (target.origin !== base.origin || target.pathname !== '/') {
    return null
  }
  const path = parseDocParam(target.searchParams.get('doc'))
  if (!path) {
    return null
  }
  return {
    path,
    hash: decodeFragment(target.hash),
  }
}
