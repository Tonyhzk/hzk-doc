import type { TocItem } from '@/api/types'

export function pageToc(items: TocItem[], title: string): TocItem[] {
  const headings = items.filter((item) => item.level >= 1 && item.level <= 3)
  const normalizedTitle = normalize(title)
  const titleIndex = headings.findIndex(
    (item) => item.level === 1 && normalize(item.text) === normalizedTitle,
  )
  if (titleIndex === -1) {
    return headings
  }
  return [
    ...headings.slice(0, titleIndex),
    ...headings.slice(titleIndex + 1),
  ]
}

export function prepareArticle(root: HTMLElement, title: string): void {
  const firstHeading = root.querySelector('h1')
  if (
    firstHeading &&
    normalize(firstHeading.textContent ?? '') === normalize(title)
  ) {
    firstHeading.remove()
  }

  for (const table of Array.from(root.querySelectorAll('table'))) {
    if (table.parentElement?.classList.contains('table-scroll')) {
      continue
    }
    const wrapper = root.ownerDocument.createElement('div')
    wrapper.className = 'table-scroll'
    table.replaceWith(wrapper)
    wrapper.append(table)
  }

  for (const image of Array.from(root.querySelectorAll('img'))) {
    image.classList.add('article-image')
    image.setAttribute('loading', 'lazy')
    if (!image.getAttribute('alt')) {
      image.setAttribute('alt', '')
    }
  }

  for (const pre of Array.from(root.querySelectorAll('pre'))) {
    pre.classList.add('code-block')
  }
}

export function extractToc(root: HTMLElement): TocItem[] {
  const headings = Array.from(
    root.querySelectorAll<HTMLElement>('h1, h2, h3'),
  )
  const used = new Set<string>()
  const items: TocItem[] = []

  headings.forEach((heading, index) => {
    const text = (heading.textContent ?? '').trim()
    if (!text) {
      return
    }
    let id = heading.id.trim()
    if (!id) {
      id = `section-${index + 1}`
      heading.id = id
    }
    if (used.has(id)) {
      let suffix = 2
      while (used.has(`${id}-${suffix}`)) {
        suffix += 1
      }
      id = `${id}-${suffix}`
      heading.id = id
    }
    used.add(id)
    items.push({
      id,
      text,
      level: Number(heading.tagName.slice(1)),
    })
  })

  return items
}

export function scrollToHeading(id: string, smooth = true): void {
  if (!id) {
    return
  }
  const element = document.getElementById(id)
  if (!element) {
    return
  }
  element.scrollIntoView({
    behavior: smooth ? 'smooth' : 'auto',
    block: 'start',
  })
}

function normalize(value: string): string {
  return value.replace(/\s+/g, '')
}
