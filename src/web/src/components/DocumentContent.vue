<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { fileUrl, htmlViewUrl } from '@/api/docs'
import type { DocumentData, TocItem } from '@/api/types'
import ImageLightbox from '@/components/ImageLightbox.vue'
import OfficePreview from '@/components/OfficePreview.vue'
import { extractToc, prepareArticle, scrollToHeading } from '@/lib/article'
import { copyText } from '@/lib/clipboard'
import {
  decodeFragment,
  resolveMarkdownHref,
  resolveViewerHref,
} from '@/lib/navigation'

const props = defineProps<{
  document: DocumentData
}>()

const emit = defineEmits<{
  toc: [items: TocItem[]]
  notice: [message: string]
}>()

const router = useRouter()
const markdownRoot = ref<HTMLElement | null>(null)
const lightboxOpen = ref(false)
const lightboxSrc = ref('')
const lightboxAlt = ref('')
let renderVersion = 0

onMounted(() => {
  if (props.document.type === 'markdown') {
    void setupMarkdown()
  }
})

watch(
  () => props.document.content,
  () => {
    if (props.document.type === 'markdown') {
      void setupMarkdown()
    }
  },
)

async function setupMarkdown(): Promise<void> {
  const root = markdownRoot.value
  if (!root) {
    return
  }
  const version = ++renderVersion
  prepareArticle(root, props.document.title)

  for (const pre of Array.from(root.querySelectorAll('pre'))) {
    if (pre.querySelector('.code-copy')) {
      continue
    }
    const button = window.document.createElement('button')
    button.type = 'button'
    button.className = 'code-copy'
    button.textContent = '复制'
    button.setAttribute('aria-label', '复制代码')
    pre.append(button)
  }

  emit('toc', extractToc(root))
  await highlightBlocks(root, version)
}

async function highlightBlocks(
  root: HTMLElement,
  version: number,
): Promise<void> {
  const blocks = Array.from(root.querySelectorAll<HTMLElement>('pre code'))
  if (blocks.length === 0) {
    return
  }
  try {
    const { default: hljs } = await import('highlight.js/lib/common')
    if (version !== renderVersion) {
      return
    }
    for (const block of blocks) {
      if (!block.isConnected || block.dataset.highlighted === 'yes') {
        continue
      }
      const source = block.textContent ?? ''
      if (!source) {
        continue
      }
      const languageClass = Array.from(block.classList).find((name) =>
        name.startsWith('language-'),
      )
      const language = languageClass?.slice('language-'.length).toLowerCase()
      try {
        if (language && hljs.getLanguage(language)) {
          block.innerHTML = hljs.highlight(source, {
            language,
            ignoreIllegals: true,
          }).value
        } else if (source.length <= 6000) {
          block.innerHTML = hljs.highlightAuto(source).value
        } else {
          continue
        }
        block.classList.add('hljs')
        block.dataset.highlighted = 'yes'
      } catch {
        // 单个代码块失败时保留原始文本
      }
    }
  } catch {
    // 高亮模块加载失败时保留原始文本
  }
}

async function handleArticleClick(event: MouseEvent): Promise<void> {
  const target = event.target
  if (!(target instanceof Element)) {
    return
  }

  const copyButton = target.closest<HTMLButtonElement>('.code-copy')
  if (copyButton) {
    event.preventDefault()
    const code = copyButton.closest('pre')?.querySelector('code')
    const copied = await copyText(code?.textContent ?? '')
    copyButton.textContent = copied ? '已复制' : '失败'
    emit('notice', copied ? '代码已复制。' : '复制失败，请手动选择代码。')
    window.setTimeout(() => {
      copyButton.textContent = '复制'
    }, 1600)
    return
  }

  const image = target.closest<HTMLImageElement>('img.article-image')
  if (image) {
    event.preventDefault()
    lightboxSrc.value = image.currentSrc || image.src
    lightboxAlt.value = image.alt
    lightboxOpen.value = true
    return
  }

  const anchor = target.closest<HTMLAnchorElement>('a')
  if (!anchor) {
    return
  }
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  ) {
    return
  }

  const href = anchor.getAttribute('href')
  if (!href || anchor.hasAttribute('download')) {
    return
  }

  if (href.startsWith('#')) {
    const id = decodeFragment(href)
    if (!id) {
      return
    }
    event.preventDefault()
    scrollToHeading(id)
    const url = new URL(window.location.href)
    url.hash = encodeURIComponent(id)
    window.history.replaceState(null, '', url)
    return
  }

  const viewerLink = resolveViewerHref(href, window.location.href)
  if (viewerLink) {
    event.preventDefault()
    await navigateToDocument(viewerLink.path, viewerLink.hash)
    return
  }

  const resolved = resolveMarkdownHref(href, props.document.path)
  if (!resolved) {
    return
  }
  event.preventDefault()
  await navigateToDocument(resolved.path, resolved.hash)
}

async function navigateToDocument(path: string, heading: string): Promise<void> {
  if (path === props.document.path) {
    if (heading) {
      scrollToHeading(heading)
      const url = new URL(window.location.href)
      url.hash = encodeURIComponent(heading)
      window.history.replaceState(null, '', url)
    } else {
      const url = new URL(window.location.href)
      url.hash = ''
      window.history.replaceState(null, '', url)
      window.scrollTo({ top: 0, behavior: 'smooth' })
    }
    return
  }

  await router.push({
    path: '/',
    query: { doc: path },
    hash: heading ? `#${heading}` : '',
  })
}

function closeLightbox(): void {
  lightboxOpen.value = false
}

function openImage(): void {
  lightboxSrc.value = props.document.url || fileUrl(props.document.path)
  lightboxAlt.value = props.document.title
  lightboxOpen.value = true
}

function downloadUrl(): string {
  return fileUrl(props.document.path, true)
}
</script>

<template>
  <div
    v-if="document.type === 'markdown'"
    ref="markdownRoot"
    class="markdown-body"
    @click="handleArticleClick"
    v-html="document.content || ''"
  ></div>

  <div v-else-if="document.type === 'text'" class="text-document">
    <pre v-if="document.rawContent !== undefined" class="text-body">{{ document.rawContent }}</pre>
    <p v-else class="preview-note">
      文本内容未能加载，可以下载原文件查看。
    </p>
  </div>

  <figure v-else-if="document.type === 'image'" class="image-document">
    <button
      type="button"
      class="image-button"
      aria-label="放大查看图片"
      @click="openImage()"
    >
      <img
        :src="document.url || fileUrl(document.path)"
        :alt="document.title"
      />
    </button>
    <figcaption>
      <a :href="downloadUrl()" download>下载图片</a>
    </figcaption>
  </figure>

  <figure v-else-if="document.type === 'html'" class="html-document">
    <iframe
      class="html-frame"
      :src="document.url || htmlViewUrl(document.path)"
      :title="`${document.title}（沙箱页面）`"
      sandbox=""
      referrerpolicy="no-referrer"
    ></iframe>
    <figcaption>HTML 文档在沙箱中显示，不会访问本站页面内容。</figcaption>
  </figure>

  <figure v-else-if="document.type === 'pdf'" class="pdf-document">
    <iframe
      class="pdf-frame"
      :src="document.url || fileUrl(document.path)"
      :title="document.title"
    ></iframe>
    <figcaption>
      <a :href="fileUrl(document.path, true)" download>下载 PDF</a>
    </figcaption>
  </figure>

  <div v-else-if="document.type === 'audio'" class="media-document">
    <audio controls :src="document.url || fileUrl(document.path)">
      当前浏览器不支持音频播放。
    </audio>
    <p><a :href="downloadUrl()" download>下载音频</a></p>
  </div>

  <div v-else-if="document.type === 'video'" class="media-document">
    <video controls :src="document.url || fileUrl(document.path)">
      当前浏览器不支持视频播放。
    </video>
    <p><a :href="downloadUrl()" download>下载视频</a></p>
  </div>

  <OfficePreview
    v-else-if="
      document.type === 'docx' ||
      document.type === 'xlsx' ||
      document.type === 'pptx'
    "
    :document="document"
  />

  <div v-else class="preview-error" role="alert">
    <p>暂不支持预览此格式。</p>
    <a class="button" :href="downloadUrl()" download>下载</a>
  </div>

  <ImageLightbox
    :open="lightboxOpen"
    :src="lightboxSrc"
    :alt="lightboxAlt"
    @close="closeLightbox"
  />
</template>
