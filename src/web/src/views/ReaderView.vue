<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import {
  documentArchiveUrl,
  documentFileUrl,
  fetchDocument,
  fetchTree,
} from '@/api/docs'
import { ApiError } from '@/api/http'
import type {
  DocumentData,
  TocItem,
  TreeEntry,
  TreeFile,
  TreeFolder,
} from '@/api/types'
import AppHeader from '@/components/AppHeader.vue'
import DocumentContent from '@/components/DocumentContent.vue'
import SearchDialog from '@/components/SearchDialog.vue'
import TocNav from '@/components/TocNav.vue'
import ToastNotice from '@/components/ToastNotice.vue'
import TreeDrawer from '@/components/TreeDrawer.vue'
import TreeNav from '@/components/TreeNav.vue'
import { useNotice } from '@/composables/useNotice'
import { pageToc, scrollToHeading } from '@/lib/article'
import { copyText } from '@/lib/clipboard'
import {
  fileMark,
  formatBytes,
  formatDate,
  kindLabel,
} from '@/lib/format'
import {
  decodeFragment,
  parseIdParam,
  readingLocation,
  type DocumentTarget,
} from '@/lib/navigation'
import {
  countFolderDocuments,
  documentNeighbors,
  findFileByPath,
  flattenFiles,
  folderTrail,
} from '@/lib/tree'
import { useSiteStore } from '@/stores/site'

type DocState = 'idle' | 'loading' | 'ready' | 'notfound' | 'error'
type TreeState = 'loading' | 'ready' | 'error'

type OverviewCard =
  | { type: 'folder'; folder: TreeFolder; files: TreeFile[]; count: number }
  | { type: 'file'; file: TreeFile }

const route = useRoute()
const router = useRouter()
const { site, loadSite } = useSiteStore()
const { message, notify } = useNotice()

const tree = ref<TreeEntry[]>([])
const treeState = ref<TreeState>('loading')
const treeError = ref('')
const document = ref<DocumentData | null>(null)
const docState = ref<DocState>('idle')
const docError = ref('')
const toc = ref<TocItem[]>([])
const articleElement = ref<HTMLElement | null>(null)
const downloadMenu = ref<HTMLDetailsElement | null>(null)
const searchOpen = ref(false)
const drawerOpen = ref(false)
let docController: AbortController | null = null

const currentId = computed(() => parseIdParam(route.query.id))
const currentPath = computed(() => document.value?.path ?? '')
const neighbors = computed(() =>
  documentNeighbors(tree.value, document.value?.id ?? currentId.value),
)
const currentFolders = computed(() => folderTrail(currentPath.value))
const effectiveToc = computed<TocItem[]>(() => {
  const current = document.value
  if (!current || current.type !== 'markdown') {
    return []
  }
  const backendToc = pageToc(current.toc ?? [], current.title)
  return backendToc.length > 0 ? backendToc : toc.value
})
const showToc = computed(
  () =>
    document.value?.type === 'markdown' && effectiveToc.value.length > 0,
)
const wideDocument = computed(() =>
  ['pdf', 'docx', 'xlsx', 'pptx', 'video', 'html'].includes(
    document.value?.type ?? '',
  ),
)
const noTocLayout = computed(
  () =>
    Boolean(currentId.value) &&
    (document.value
      ? document.value.type !== 'markdown'
      : docState.value !== 'loading'),
)
const docMeta = computed(() => {
  const current = document.value
  if (!current) {
    return []
  }
  const parts = [kindLabel(current.type)]
  const size =
    current.size ??
    (current.rawContent !== undefined
      ? new TextEncoder().encode(current.rawContent).length
      : undefined)
  const sizeLabel = formatBytes(size)
  if (sizeLabel) {
    parts.push(sizeLabel)
  }
  const dateLabel = formatDate(current.modifiedAt)
  if (dateLabel) {
    parts.push(dateLabel)
  }
  return parts
})
const overviewCards = computed<OverviewCard[]>(() =>
  tree.value.map((entry) => {
    if (entry.type === 'folder') {
      return {
        type: 'folder',
        folder: entry,
        files: flattenFiles(entry.children).slice(0, 4),
        count: countFolderDocuments(entry),
      }
    }
    return { type: 'file', file: entry }
  }),
)
const canDownload = computed(() => {
  const current = document.value
  if (!current || current.type === 'html') {
    return false
  }
  if (current.rawContent !== undefined) {
    return true
  }
  return ['pdf', 'docx', 'xlsx', 'pptx', 'text', 'image', 'audio', 'video'].includes(
    current.type,
  )
})

watch(
  currentId,
  (id) => {
    drawerOpen.value = false
    void loadDocument(id)
  },
  { immediate: true },
)

watch(
  () => route.hash,
  async () => {
    if (docState.value !== 'ready') {
      return
    }
    await nextTick()
    scrollToHash()
  },
)

watch(
  () => site.name,
  (name) => {
    if (document.value) {
      window.document.title = `${document.value.title} · ${name}`
    } else if (!currentId.value) {
      window.document.title = name
    }
  },
)

function readingLink(id: string) {
  return readingLocation(id)
}

async function loadTree(): Promise<void> {
  treeState.value = 'loading'
  treeError.value = ''
  try {
    tree.value = await fetchTree()
    treeState.value = 'ready'
  } catch (error) {
    treeState.value = 'error'
    treeError.value =
      error instanceof Error ? error.message : '目录加载失败，请稍后重试。'
  }
}

async function loadDocument(id: string): Promise<void> {
  docController?.abort()
  docController = null
  if (document.value && document.value.id !== id) {
    articleElement.value?.querySelectorAll<HTMLMediaElement>('audio, video').forEach((media) => {
      media.pause()
    })
  }

  if (!id) {
    toc.value = []
    document.value = null
    docState.value = 'idle'
    docError.value = ''
    window.document.title = site.name
    return
  }

  docState.value = 'loading'
  docError.value = ''
  const controller = new AbortController()
  docController = controller

  try {
    const data = await fetchDocument(id, controller.signal)
    if (controller.signal.aborted) {
      return
    }
    toc.value = []
    document.value = data
    docState.value = 'ready'
    window.document.title = `${data.title} · ${site.name}`
    await nextTick()
    if (!controller.signal.aborted) {
      scrollToHash()
    }
  } catch (error) {
    if (controller.signal.aborted) {
      return
    }
    if (error instanceof DOMException && error.name === 'AbortError') {
      return
    }
    toc.value = []
    document.value = null
    docError.value =
      error instanceof Error ? error.message : '文档加载失败，请稍后重试。'
    docState.value =
      error instanceof ApiError && error.status === 404 ? 'notfound' : 'error'
    window.document.title =
      docState.value === 'notfound'
        ? `文档不存在 · ${site.name}`
        : `加载失败 · ${site.name}`
  }
}

function scrollToHash(): void {
  const id = decodeFragment(route.hash)
  if (!id) {
    return
  }
  window.requestAnimationFrame(() => {
    scrollToHeading(id, false)
  })
}

function openDocument(id: string, heading = ''): void {
  searchOpen.value = false
  drawerOpen.value = false
  if (id === currentId.value) {
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
  void router.push(readingLocation(id, heading))
}

function retryDocument(): void {
  void loadDocument(currentId.value)
}

function navigateToTarget(target: DocumentTarget): void {
  let id = target.id ?? ''
  if (!id && target.path) {
    id = findFileByPath(tree.value, target.path)?.id ?? ''
  }
  if (!id) {
    notify('没有找到链接指向的文档。')
    return
  }
  openDocument(id, target.heading)
}

async function copyFullText(): Promise<void> {
  const current = document.value
  if (!current || current.rawContent === undefined) {
    return
  }
  if (!current.rawContent) {
    notify('文档没有可复制的内容。')
    return
  }
  const copied = await copyText(current.rawContent)
  notify(copied ? '全文已复制。' : '复制失败，请手动选择文本。')
}

async function sharePage(): Promise<void> {
  const copied = await copyText(window.location.href)
  notify(copied ? '链接已复制。' : '复制失败，请从地址栏复制链接。')
}

function fileBasename(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1) || 'document'
}

function downloadHrefFor(current: DocumentData): string {
  return documentFileUrl(current.id, true)
}

function downloadDocument(): void {
  const current = document.value
  if (!current || current.rawContent === undefined) {
    return
  }
  const mime =
    current.type === 'markdown'
      ? 'text/markdown;charset=utf-8'
      : 'text/plain;charset=utf-8'
  const blob = new Blob([current.rawContent], { type: mime })
  const url = URL.createObjectURL(blob)
  const anchor = window.document.createElement('a')
  anchor.href = url
  anchor.download = fileBasename(current.path)
  window.document.body.append(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function onDocumentClick(event: MouseEvent): void {
  const menu = downloadMenu.value
  if (menu?.open && event.target instanceof Node && !menu.contains(event.target)) {
    menu.open = false
  }
}

function onGlobalKeydown(event: KeyboardEvent): void {
  if (
    (event.metaKey || event.ctrlKey) &&
    event.key.toLowerCase() === 'k'
  ) {
    if (!searchOpen.value && window.document.querySelector('dialog[open]')) {
      return
    }
    event.preventDefault()
    searchOpen.value = true
  }
}

onMounted(() => {
  void loadSite()
  void loadTree()
  window.document.addEventListener('click', onDocumentClick)
  window.addEventListener('keydown', onGlobalKeydown)
})

onBeforeUnmount(() => {
  window.document.removeEventListener('click', onDocumentClick)
  window.removeEventListener('keydown', onGlobalKeydown)
  docController?.abort()
})
</script>

<template>
  <div class="reader-page">
    <AppHeader
      @open-nav="drawerOpen = true"
      @search="searchOpen = true"
    />

    <div
      class="reader-layout"
      :class="{
        'is-home': !currentId,
        'no-toc': noTocLayout,
      }"
    >
      <aside class="sidebar" aria-label="文档导航">
        <p class="sidebar-title">文档目录</p>
        <p v-if="treeState === 'loading'" class="sidebar-note">
          正在读取目录…
        </p>
        <div v-else-if="treeState === 'error'" class="sidebar-note">
          <p>{{ treeError }}</p>
          <button type="button" class="button button-quiet" @click="loadTree">
            重试
          </button>
        </div>
        <TreeNav
          v-else
          :entries="tree"
          :current-id="currentId"
          @select="openDocument"
        />
      </aside>

      <main
        id="main-content"
        class="reader-main"
        tabindex="-1"
        :aria-busy="docState === 'loading'"
      >
        <div v-if="docState === 'loading' && document" class="document-loading" role="status">
          正在加载文档…
        </div>
        <section v-if="!currentId" class="overview">
          <header class="overview-header">
            <h1>{{ site.name }}</h1>
            <p class="overview-lead">
              共 {{ site.count }} 篇文档。从文档目录选择内容，或按 ⌘/Ctrl + K
              搜索全部内容。
            </p>
          </header>

          <div
            v-if="treeState === 'loading'"
            class="state-panel"
            role="status"
          >
            正在读取文档目录…
          </div>

          <div
            v-else-if="treeState === 'error'"
            class="state-panel"
            role="alert"
          >
            <h2>目录加载失败</h2>
            <p>{{ treeError }}</p>
            <button type="button" class="button button-primary" @click="loadTree">
              重试
            </button>
          </div>

          <div v-else-if="overviewCards.length === 0" class="state-panel">
            <h2>文档目录为空</h2>
            <p>向文档目录中添加 Markdown、HTML 或文本文件后即可开始阅读。</p>
            <RouterLink v-if="site.adminEnabled" class="button button-primary" to="/admin">
              管理文档
            </RouterLink>
          </div>

          <ul v-else class="overview-list">
            <li v-for="card in overviewCards" :key="card.type === 'folder' ? card.folder.path : card.file.path">
              <article v-if="card.type === 'folder'" class="overview-card">
                <div class="overview-card-head">
                  <h2>{{ card.folder.name }}</h2>
                  <span>{{ card.count }} 篇</span>
                </div>
                <ul class="folder-docs">
                  <li v-for="file in card.files" :key="file.id">
                    <RouterLink :to="readingLink(file.id)">
                      {{ file.displayName || file.name }}
                    </RouterLink>
                    <span class="doc-kind">
                      {{ kindLabel(file.fileTypeCategory) }}
                    </span>
                  </li>
                </ul>
                <p v-if="card.count > card.files.length" class="overview-more">
                  另有 {{ card.count - card.files.length }} 篇，请在文档目录中查看。
                </p>
              </article>

              <RouterLink
                v-else
                class="overview-card overview-file"
                :to="readingLink(card.file.id)"
              >
                <span class="file-mark" aria-hidden="true">
                  {{ fileMark(card.file.fileTypeCategory, card.file.path) }}
                </span>
                <span class="overview-file-name">
                  {{ card.file.displayName || card.file.name }}
                </span>
                <span class="doc-kind">
                  {{ kindLabel(card.file.fileTypeCategory) }}
                </span>
              </RouterLink>
            </li>
          </ul>
        </section>

        <div
          v-else-if="docState === 'loading' && !document"
          class="article-skeleton"
          aria-busy="true"
          aria-label="正在加载文档"
        >
          <span class="skeleton-title"></span>
          <span class="skeleton-line"></span>
          <span class="skeleton-line"></span>
          <span class="skeleton-line short"></span>
          <span class="skeleton-line"></span>
        </div>

        <section v-else-if="docState === 'notfound'" class="state-panel">
          <h1>文档不存在</h1>
          <p>这个地址没有对应的文档，可能已被移动或删除。</p>
          <div class="state-actions">
            <RouterLink class="button button-primary" to="/">返回首页</RouterLink>
            <button type="button" class="button" @click="searchOpen = true">
              搜索
            </button>
          </div>
        </section>

        <section v-else-if="docState === 'error'" class="state-panel" role="alert">
          <h1>文档加载失败</h1>
          <p>{{ docError }}</p>
          <div class="state-actions">
            <button type="button" class="button button-primary" @click="retryDocument">
              重试
            </button>
            <RouterLink class="button" to="/">返回首页</RouterLink>
          </div>
        </section>

        <article
          v-else-if="document"
          ref="articleElement"
          class="article"
          :class="{ 'article-wide': wideDocument }"
          :inert="docState === 'loading' ? true : undefined"
        >
          <nav class="breadcrumbs" aria-label="面包屑">
            <RouterLink to="/">文档</RouterLink>
            <template v-for="folder in currentFolders" :key="folder.path">
              <span class="crumb-separator" aria-hidden="true">/</span>
              <span>{{ folder.name }}</span>
            </template>
            <span class="crumb-separator" aria-hidden="true">/</span>
            <span class="crumb-current" aria-current="page">
              {{ document.title }}
            </span>
          </nav>

          <header class="article-header">
            <h1>{{ document.title }}</h1>
            <div class="article-meta">
              <span v-for="part in docMeta" :key="part">{{ part }}</span>
            </div>
            <div class="article-actions">
              <button
                v-if="document.rawContent !== undefined"
                type="button"
                class="button button-quiet"
                @click="copyFullText"
              >
                复制全文
              </button>
              <button
                type="button"
                class="button button-quiet"
                @click="sharePage"
              >
                分享
              </button>
              <details
                v-if="
                  canDownload &&
                  document.type === 'markdown' &&
                  document.rawContent !== undefined &&
                  document.hasAssets
                "
                ref="downloadMenu"
                class="download-menu"
              >
                <summary class="button button-quiet">下载</summary>
                <div class="download-options">
                  <button
                    type="button"
                    class="button button-quiet"
                    @click="downloadDocument(); downloadMenu!.open = false"
                  >
                    仅 Markdown
                  </button>
                  <a
                    class="button button-quiet"
                    :href="documentArchiveUrl(document.id)"
                    download
                    @click="downloadMenu!.open = false"
                  >
                    含素材 ZIP
                  </a>
                </div>
              </details>
              <button
                v-else-if="canDownload && document.type === 'markdown' && document.rawContent !== undefined"
                type="button"
                class="button button-quiet"
                @click="downloadDocument"
              >
                下载
              </button>
              <a
                v-else-if="canDownload"
                class="button button-quiet"
                :href="downloadHrefFor(document)"
                download
              >
                下载
              </a>
            </div>
          </header>

          <details v-if="showToc" class="toc-details">
            <summary>本页目录</summary>
            <TocNav :items="effectiveToc" />
          </details>

          <div class="article-body">
            <DocumentContent
              :key="document.id"
              :document="document"
              @toc="toc = $event"
              @notice="notify"
              @navigate="navigateToTarget"
            />
          </div>

          <nav
            v-if="neighbors.previous || neighbors.next"
            class="pager"
            aria-label="文档翻页"
          >
            <RouterLink
              v-if="neighbors.previous"
              class="pager-link"
              :to="readingLink(neighbors.previous.id)"
            >
              <span class="pager-label">上一篇</span>
              <span class="pager-title">
                {{ neighbors.previous.displayName || neighbors.previous.name }}
              </span>
            </RouterLink>
            <span v-else></span>
            <RouterLink
              v-if="neighbors.next"
              class="pager-link pager-next"
              :to="readingLink(neighbors.next.id)"
            >
              <span class="pager-label">下一篇</span>
              <span class="pager-title">
                {{ neighbors.next.displayName || neighbors.next.name }}
              </span>
            </RouterLink>
          </nav>
        </article>
      </main>

      <aside v-if="showToc" class="toc-rail" :inert="docState === 'loading' ? true : undefined">
        <TocNav :items="effectiveToc" />
      </aside>
    </div>

    <footer class="site-footer">
      <span>{{ site.name }}</span>
      <span v-if="site.version">版本 {{ site.version }}</span>
    </footer>

    <TreeDrawer
      v-model:open="drawerOpen"
      :entries="tree"
      :current-id="currentId"
      @select="openDocument"
    />
    <SearchDialog
      :open="searchOpen"
      @close="searchOpen = false"
      @select="openDocument"
    />
    <ToastNotice :message="message" />
  </div>
</template>
