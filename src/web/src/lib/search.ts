export interface SnippetSegment {
  text: string
  match: boolean
}

export function parseSnippet(input: string): SnippetSegment[] {
  if (!input) {
    return []
  }

  const segments: SnippetSegment[] = []
  const pattern = /\[\[([\s\S]*?)\]\]/g
  let cursor = 0
  let match: RegExpExecArray | null

  while ((match = pattern.exec(input)) !== null) {
    if (match.index > cursor) {
      segments.push({ text: input.slice(cursor, match.index), match: false })
    }
    if (match[1]) {
      segments.push({ text: match[1], match: true })
    }
    cursor = match.index + match[0].length
  }

  if (cursor < input.length) {
    segments.push({ text: input.slice(cursor), match: false })
  }
  if (segments.length === 0 && input) {
    segments.push({ text: input, match: false })
  }
  return segments
}
