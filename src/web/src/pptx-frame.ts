import { init } from 'pptx-preview'

declare global {
  interface Window {
    __PPTX_CHANNEL__?: unknown
  }
}

const channel = window.__PPTX_CHANNEL__
if (typeof channel === 'string' && channel) {
  start(channel)
}

function start(channel: string): void {
  const element = document.getElementById('stage')
  let buffer: ArrayBuffer | null = null
  let renderId = 0

  function send(type: string, payload?: Record<string, unknown>): void {
    window.parent.postMessage({ channel, type, ...payload }, '*')
  }

  function fail(message: unknown): void {
    send('error', {
      message: String(message || 'PPTX 渲染失败。'),
    })
  }

  if (!element) {
    fail('PPTX 预览容器缺失。')
    return
  }
  const stage: HTMLElement = element

  window.addEventListener('message', (event) => {
    if (event.source !== window.parent) {
      return
    }
    const data = event.data as unknown
    if (!data || typeof data !== 'object') {
      return
    }
    const record = data as Record<string, unknown>
    if (record.channel !== channel || record.type !== 'render') {
      return
    }
    if (!(record.buffer instanceof ArrayBuffer)) {
      fail('PPTX 数据格式无效。')
      return
    }
    buffer = record.buffer
    void render()
  })

  async function render(): Promise<void> {
    const current = ++renderId
    const data = buffer
    if (!data) {
      return
    }

    try {
      stage.replaceChildren()
      const width = Math.min(
        Math.max(document.documentElement.clientWidth || 0, 320),
        1200,
      )
      const height = Math.round((width * 9) / 16)
      const previewer = init(stage, { width, height })
      const pending = previewer.preview(data)
      const pptx = previewer.pptx
      const loadPresentation = pptx._loadPresentation.bind(pptx)
      // 库在后续解析时对可缺省的默认文本样式直接调用 Object.keys。
      pptx._loadPresentation = async () => {
        await loadPresentation()
        pptx.defaultTextStyleSource ??= {}
      }
      await pending
      if (current !== renderId) {
        return
      }
      const rect = stage.getBoundingClientRect()
      const measured = Math.max(
        rect.height || 0,
        stage.scrollHeight || 0,
        height,
      )
      send('rendered', { height: measured })
    } catch (error) {
      if (current !== renderId) {
        return
      }
      fail(error instanceof Error ? error.message : 'PPTX 渲染失败。')
    }
  }
}
