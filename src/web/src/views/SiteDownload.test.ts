import { afterEach, describe, expect, it, vi } from 'vitest'

import { saveDownloadEnabled, setAdminToken } from '@/api/admin'
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
