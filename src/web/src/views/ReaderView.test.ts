// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import type { App } from 'vue'

import type { DocumentData, TocItem } from '@/api/types'
import { ApiError } from '@/api/http'
import ReaderView from '@/views/ReaderView.vue'

const mocks = vi.hoisted(() => ({
  fetchDocument: vi.fn(),
  fetchTree: vi.fn(),
  route: { query: { id: '' }, hash: '' },
}))

vi.mock('vue-router', () => ({
  useRoute: () => mocks.route,
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/api/docs', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/docs')>()),
  fetchDocument: mocks.fetchDocument,
  fetchTree: mocks.fetchTree,
}))

vi.mock('@/stores/site', () => ({
  useSiteStore: () => ({
    site: { name: '测试站点', count: 0, adminEnabled: false },
    loadSite: vi.fn(),
  }),
}))

vi.mock('@/components/AppHeader.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/SearchDialog.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/ToastNotice.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/TreeDrawer.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/TreeNav.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/TocNav.vue', () => ({
  default: defineComponent({
    props: { items: { type: Array<TocItem>, required: true } },
    setup: (props) => () => h('div', { 'data-toc': props.items.map((item) => item.id).join(',') }),
  }),
}))
vi.mock('@/components/DocumentContent.vue', () => ({
  default: defineComponent({
    props: { document: { type: Object as () => DocumentData, required: true } },
    setup: (props) => () => h('div', { 'data-doc-id': props.document.id }),
  }),
}))

interface Request {
  signal: AbortSignal
  resolve: (document: DocumentData) => void
  reject: (error: Error) => void
}

const requests: Request[] = []
const apps: App[] = []

function documentData(id: string, type = 'markdown'): DocumentData {
  return {
    id,
    type,
    path: `${id}.md`,
    title: id,
    toc: type === 'markdown' ? [{ id: `${id}-heading`, text: id, level: 2 }] : [],
  }
}

async function flush(): Promise<void> {
  await Promise.resolve()
  await nextTick()
}

function mount(id: string): HTMLElement {
  mocks.route = reactive({ query: { id }, hash: '' })
  const root = document.createElement('div')
  document.body.append(root)
  const app = createApp(ReaderView)
  app.component('RouterLink', defineComponent({
    setup: (_props, { slots }) => () => h('a', slots.default?.()),
  }))
  app.mount(root)
  apps.push(app)
  return root
}

beforeEach(() => {
  requests.length = 0
  mocks.fetchDocument.mockReset()
  mocks.fetchTree.mockReset()
  mocks.fetchTree.mockResolvedValue([])
  mocks.fetchDocument.mockImplementation((_id: string, signal: AbortSignal) =>
    new Promise<DocumentData>((resolve, reject) => {
      requests.push({ signal, resolve, reject })
    }),
  )
})

afterEach(() => {
  for (const app of apps.splice(0)) app.unmount()
  document.body.replaceChildren()
  vi.restoreAllMocks()
})

describe('阅读页文档切换', () => {
  it('首次加载显示骨架，切换时保留正文与目录并暂停媒体', async () => {
    const root = mount('a')
    expect(root.querySelector('.article-skeleton')).not.toBeNull()
    expect(root.querySelector('.article')).toBeNull()

    requests[0]!.resolve(documentData('a'))
    await flush()
    const article = root.querySelector<HTMLElement>('.article')!
    const toc = root.querySelector<HTMLElement>('.toc-rail')!
    const audio = document.createElement('audio')
    const pause = vi.spyOn(audio, 'pause').mockImplementation(() => {})
    article.append(audio)
    expect(article).not.toBeNull()
    expect(toc).not.toBeNull()
    expect(root.querySelector('.article-skeleton')).toBeNull()

    mocks.route.query.id = 'b'
    await flush()
    expect(pause).toHaveBeenCalledOnce()
    expect(root.querySelector('.article')).toBe(article)
    expect(root.querySelector('.toc-rail')).toBe(toc)
    expect(root.querySelector('.article-skeleton')).toBeNull()
    expect(root.querySelector('.document-loading')).not.toBeNull()
    expect(root.querySelector('.reader-main')?.getAttribute('aria-busy')).toBe('true')
    expect(article.hasAttribute('inert')).toBe(true)
    expect(toc.hasAttribute('inert')).toBe(true)
    expect(root.querySelector('[data-doc-id="a"]')).not.toBeNull()

    requests[1]!.resolve(documentData('b'))
    await flush()
    expect(root.querySelector('.article')).toBe(article)
    expect(root.querySelector('.document-loading')).toBeNull()
    expect(root.querySelector('.reader-main')?.getAttribute('aria-busy')).toBe('false')
    expect(article.hasAttribute('inert')).toBe(false)
    expect(toc.hasAttribute('inert')).toBe(false)
    expect(root.querySelector('[data-doc-id="b"]')).not.toBeNull()
    expect(root.querySelector('[data-toc="b-heading"]')).not.toBeNull()

    const layout = root.querySelector('.reader-layout')!
    mocks.route.query.id = 'pdf'
    await flush()
    expect(layout.classList.contains('no-toc')).toBe(false)
    expect(root.querySelector('.toc-rail')).toBe(toc)
    requests[2]!.resolve(documentData('pdf', 'pdf'))
    await flush()
    expect(layout.classList.contains('no-toc')).toBe(true)
    expect(root.querySelector('.toc-rail')).toBeNull()
    expect(root.querySelector('.article-wide')).toBe(article)
  })

  it('连续切换时忽略过期结果，失败或返回首页后清除旧正文', async () => {
    const root = mount('a')
    requests[0]!.resolve(documentData('a', 'pdf'))
    await flush()
    const layout = root.querySelector('.reader-layout')!
    expect(layout.classList.contains('no-toc')).toBe(true)

    mocks.route.query.id = 'b'
    await flush()
    expect(layout.classList.contains('no-toc')).toBe(true)
    mocks.route.query.id = 'c'
    await flush()
    expect(requests[1]!.signal.aborted).toBe(true)
    requests[1]!.resolve(documentData('b'))
    await flush()
    expect(root.querySelector('[data-doc-id="a"]')).not.toBeNull()
    expect(layout.classList.contains('no-toc')).toBe(true)

    requests[2]!.resolve(documentData('c'))
    await flush()
    expect(root.querySelector('[data-doc-id="c"]')).not.toBeNull()
    expect(layout.classList.contains('no-toc')).toBe(false)
    expect(root.querySelector('.toc-rail')).not.toBeNull()

    mocks.route.query.id = 'missing'
    await flush()
    requests[3]!.reject(new ApiError('未找到', 404))
    await flush()
    expect(root.querySelector('.article')).toBeNull()
    expect(root.querySelector('.toc-rail')).toBeNull()
    expect(root.querySelector('.state-panel')).not.toBeNull()

    mocks.route.query.id = 'pending'
    await flush()
    mocks.route.query.id = ''
    await flush()
    expect(requests[4]!.signal.aborted).toBe(true)
    requests[4]!.resolve(documentData('pending'))
    await flush()
    expect(root.querySelector('.overview')).not.toBeNull()
    expect(root.querySelector('.article')).toBeNull()
  })
})
