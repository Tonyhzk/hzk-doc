import { afterEach, describe, expect, it, vi } from 'vitest'

import { saveArticleMeta, saveDownloadEnabled, setAdminToken } from '@/api/admin'
import { fetchSite } from '@/api/site'

function response(data: unknown): Response {
  return new Response(JSON.stringify({ success: true, data }), {
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  setAdminToken(undefined)
  vi.unstubAllGlobals()
})

describe('文章信息设置接口', () => {
  it('缺失或不是布尔 true 时默认隐藏格式和大小', async () => {
    const fetchMock = vi.fn(async () => response({}))
    fetchMock.mockResolvedValueOnce(response({}))
    fetchMock.mockResolvedValueOnce(response({ showFileType: 'true', showFileSize: 1 }))
    fetchMock.mockResolvedValueOnce(response({ showFileType: true, showFileSize: false }))
    vi.stubGlobal('fetch', fetchMock)

    const initial = await fetchSite()
    expect(initial.showFileType).toBe(false)
    expect(initial.showFileSize).toBe(false)
    const invalid = await fetchSite()
    expect(invalid.showFileType).toBe(false)
    expect(invalid.showFileSize).toBe(false)
    const enabled = await fetchSite()
    expect(enabled.showFileType).toBe(true)
    expect(enabled.showFileSize).toBe(false)
  })

  it.each([
    { showFileType: false, showFileSize: false },
    { showFileType: true, showFileSize: false },
    { showFileType: false, showFileSize: true },
    { showFileType: true, showFileSize: true },
  ])('提交文章信息显示组合 $showFileType / $showFileSize', async (settings) => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => response(settings))
    vi.stubGlobal('fetch', fetchMock)
    setAdminToken('test-token')

    expect(await saveArticleMeta(settings)).toEqual(settings)
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/admin/article-meta')
    expect(init?.method).toBe('POST')
    expect(init?.credentials).toBe('same-origin')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('test-token')
    expect(JSON.parse(String(init?.body))).toEqual(settings)
  })
})

describe('下载设置接口', () => {
  it('缺少开关或开关不是 true 时默认关闭', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => response({}))
    fetchMock.mockResolvedValueOnce(response({}))
    fetchMock.mockResolvedValueOnce(response({ downloadEnabled: 'true' }))
    fetchMock.mockResolvedValueOnce(response({ downloadEnabled: true }))
    vi.stubGlobal('fetch', fetchMock)

    expect((await fetchSite()).downloadEnabled).toBe(false)
    expect((await fetchSite()).downloadEnabled).toBe(false)
    expect((await fetchSite()).downloadEnabled).toBe(true)
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/site')
  })

  it('保存开关时发送布尔值和管理员 CSRF 令牌', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      response({ downloadEnabled: true }),
    )
    vi.stubGlobal('fetch', fetchMock)
    setAdminToken('test-token')

    expect(await saveDownloadEnabled(true)).toEqual({ downloadEnabled: true })
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/admin/download')
    expect(init?.method).toBe('POST')
    expect(init?.credentials).toBe('same-origin')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('test-token')
    expect(JSON.parse(String(init?.body))).toEqual({ downloadEnabled: true })
  })
})
