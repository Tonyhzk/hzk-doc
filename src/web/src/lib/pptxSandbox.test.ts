import { describe, expect, it } from 'vitest'

import { buildPptxDocument, renderPptxSandbox } from '@/lib/pptxSandbox'

describe('buildPptxDocument', () => {
  it('转义脚本中的结束标签', () => {
    const html = buildPptxDocument(
      'channel-1',
      'const sample = "</script><img src=x>"',
    )

    expect(html).not.toContain('"</script><img src=x>"')
    expect(html).toContain('<\\/script>')
  })

  it('转义通道标识中的引号与换行', () => {
    const html = buildPptxDocument('a"b\nc', 'window.pptxPreview = {}')

    expect(html).toContain('window.__PPTX_CHANNEL__ = "a\\"b\\nc";')
  })

  it('限制沙箱内的网络、表单与外部资源', () => {
    const html = buildPptxDocument('channel-2', 'window.pptxPreview = {}')

    expect(html).toContain("default-src 'none'")
    expect(html).toContain("script-src 'unsafe-inline'")
    expect(html).toContain("connect-src 'none'")
    expect(html).toContain("form-action 'none'")
    expect(html).toContain('img-src data: blob:')
    expect(html).toContain('font-src data: blob:')
  })
})

describe('renderPptxSandbox', () => {
  it('信号已取消时不创建预览', async () => {
    const controller = new AbortController()
    controller.abort()

    await expect(
      renderPptxSandbox({
        container: {} as HTMLElement,
        script: '',
        buffer: new ArrayBuffer(0),
        title: '测试文档',
        signal: controller.signal,
      }),
    ).rejects.toMatchObject({ name: 'AbortError' })
  })
})
