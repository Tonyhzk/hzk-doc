const PPTX_TIMEOUT = 30_000

const PPTX_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  'img-src data: blob:',
  'font-src data: blob:',
  'media-src data: blob:',
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "object-src 'none'",
  "frame-src 'none'",
].join('; ')

export interface PptxSandboxOptions {
  container: HTMLElement
  script: string
  buffer: ArrayBuffer
  title: string
  signal: AbortSignal
}

export interface PptxSandboxHandle {
  cleanup(): void
}

export function buildPptxDocument(channel: string, script: string): string {
  const safeScript = escapeScriptText(script)
  const safeChannel = JSON.stringify(channel)

  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${PPTX_CSP}">
<meta name="referrer" content="no-referrer">
<style>
  html, body { margin: 0; padding: 0; background: transparent; }
  body { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; }
  #stage { width: 100%; }
  #stage canvas { max-width: 100%; height: auto !important; }
</style>
</head>
<body>
<div id="stage"></div>
<script>window.__PPTX_CHANNEL__ = ${safeChannel};</script>
<script>${safeScript}</script>
</body>
</html>`
}

function escapeScriptText(value: string): string {
  return value.replace(/<\/script/gi, (match) => `<\\/${match.slice(2)}`)
}

export function renderPptxSandbox(
  options: PptxSandboxOptions,
): Promise<PptxSandboxHandle> {
  const { container, script, buffer, title, signal } = options

  if (signal.aborted) {
    return Promise.reject(
      new DOMException('已取消 PPTX 预览。', 'AbortError'),
    )
  }

  container.replaceChildren()

  const channel = createChannel()
  const html = buildPptxDocument(channel, script)
  const blobUrl = URL.createObjectURL(new Blob([html], { type: 'text/html' }))

  const iframe = document.createElement('iframe')
  iframe.className = 'pptx-frame'
  iframe.title = `${title}（PPTX 沙箱预览）`
  iframe.setAttribute('sandbox', 'allow-scripts')
  iframe.setAttribute('referrerpolicy', 'no-referrer')
  iframe.src = blobUrl

  const initialWidth = Math.max(container.clientWidth || 0, 320)
  iframe.style.height = `${Math.min(Math.round((initialWidth * 9) / 16), 200_000)}px`

  return new Promise((resolve, reject) => {
    let settled = false
    let timer = 0

    const cleanupListeners = (): void => {
      window.removeEventListener('message', onMessage)
      iframe.removeEventListener('load', onLoad)
      signal.removeEventListener('abort', onAbort)
      window.clearTimeout(timer)
    }

    const destroy = (): void => {
      cleanupListeners()
      iframe.remove()
      URL.revokeObjectURL(blobUrl)
      container.replaceChildren()
    }

    const finish = (handle: PptxSandboxHandle): void => {
      if (settled) {
        return
      }
      settled = true
      cleanupListeners()
      resolve(handle)
    }

    const fail = (error: Error): void => {
      if (settled) {
        return
      }
      settled = true
      destroy()
      reject(error)
    }

    const onAbort = (): void => {
      fail(new DOMException('已取消 PPTX 预览。', 'AbortError'))
    }

    const onLoad = (): void => {
      if (settled) {
        return
      }
      const target = iframe.contentWindow
      if (!target) {
        fail(new Error('PPTX 预览窗口初始化失败。'))
        return
      }
      try {
        target.postMessage({ type: 'render', channel, buffer }, '*', [buffer])
      } catch {
        fail(new Error('PPTX 数据发送失败。'))
      }
    }

    const onMessage = (event: MessageEvent): void => {
      if (settled || event.source !== iframe.contentWindow) {
        return
      }
      const data = event.data
      if (!data || typeof data !== 'object') {
        return
      }
      const record = data as Record<string, unknown>
      if (record.channel !== channel) {
        return
      }

      if (record.type === 'rendered') {
        const height = Number(record.height)
        if (!Number.isFinite(height) || height <= 0) {
          fail(new Error('PPTX 预览高度无效。'))
          return
        }
        iframe.style.height = `${Math.min(
          Math.max(Math.round(height), 160),
          200_000,
        )}px`
        finish({ cleanup: destroy })
        return
      }

      if (record.type === 'error') {
        const message =
          typeof record.message === 'string'
            ? record.message.slice(0, 500)
            : 'PPTX 渲染失败。'
        fail(new Error(message))
      }
    }

    window.addEventListener('message', onMessage)
    iframe.addEventListener('load', onLoad, { once: true })
    signal.addEventListener('abort', onAbort, { once: true })
    container.append(iframe)
    if (!settled) {
      timer = window.setTimeout(
        () => fail(new Error('PPTX 预览超时，请重试。')),
        PPTX_TIMEOUT,
      )
    }
  })
}

function createChannel(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join(
    '',
  )
}
