const units = ['B', 'KB', 'MB', 'GB']

export function formatBytes(bytes: number | undefined): string {
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes < 0) {
    return ''
  }
  if (bytes < 1024) {
    return `${bytes} B`
  }
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(digits)} ${units[unit]}`
}

const dateFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
})

export function formatDate(value: string | undefined): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return dateFormatter.format(date)
}

const kindLabels: Record<string, string> = {
  folder: '目录',
  markdown: 'Markdown',
  html: 'HTML',
  pdf: 'PDF',
  image: '图片',
  docx: 'Word',
  xlsx: '表格',
  pptx: '幻灯片',
  audio: '音频',
  video: '视频',
  text: '文本',
}

export function kindLabel(kind: string | undefined): string {
  if (!kind) {
    return '文件'
  }
  return kindLabels[kind] ?? kind.toUpperCase()
}

export function extensionOf(path: string): string {
  const name = path.slice(path.lastIndexOf('/') + 1)
  const index = name.lastIndexOf('.')
  return index === -1 ? '' : name.slice(index + 1).toLowerCase()
}

export function extensionLabel(path: string): string {
  const extension = extensionOf(path)
  return extension ? extension.toUpperCase() : '文件'
}

const fileMarks: Record<string, string> = {
  markdown: 'MD',
  html: 'HTML',
  pdf: 'PDF',
  image: 'IMG',
  docx: 'DOC',
  xlsx: 'XLS',
  pptx: 'PPT',
  audio: 'AUD',
  video: 'VID',
  text: 'TXT',
}

export function fileMark(category: string | undefined, path: string): string {
  if (category && fileMarks[category]) {
    return fileMarks[category]
  }
  return extensionOf(path).slice(0, 4).toUpperCase() || '文件'
}

export function isMarkdownPath(path: string): boolean {
  return /\.(md|markdown|mark)$/i.test(path)
}

export function isHtmlPath(path: string): boolean {
  return /\.(html|htm)$/i.test(path)
}

export function isTextPath(path: string): boolean {
  return /\.txt$/i.test(path)
}

export function isEditablePath(path: string): boolean {
  return isMarkdownPath(path) || isHtmlPath(path) || isTextPath(path)
}

const allowedUploadExtensions = new Set([
  'md',
  'markdown',
  'mark',
  'html',
  'htm',
  'pdf',
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'svg',
  'bmp',
  'ico',
  'avif',
  'docx',
  'xlsx',
  'pptx',
  'mp3',
  'wav',
  'ogg',
  'flac',
  'aac',
  'm4a',
  'oga',
  'mp4',
  'webm',
  'mov',
  'm4v',
  'ogv',
  'txt',
  'css',
  'woff',
  'woff2',
  'ttf',
  'otf',
])

export function isAllowedUpload(path: string): boolean {
  const extension = extensionOf(path)
  return extension !== '' && allowedUploadExtensions.has(extension)
}

export const uploadAccept = Array.from(allowedUploadExtensions)
  .map((extension) => `.${extension}`)
  .join(',')
