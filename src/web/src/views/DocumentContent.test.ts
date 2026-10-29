// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import type { App } from 'vue'

import type { DocumentData } from '@/api/types'
import DocumentContent from '@/components/DocumentContent.vue'

let app: App
let root: HTMLElement

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
  root = document.createElement('div')
})

afterEach(() => {
  app.unmount()
  root.remove()
  vi.unstubAllGlobals()
})

describe('文档预览下载设置', () => {
  it.each(['image', 'pdf', 'audio', 'video', 'docx', 'xlsx', 'pptx', 'unknown'])(
    '%s 预览跟随下载设置显示入口',
    async (type) => {
      const state = reactive({ enabled: false })
      const doc: DocumentData = { id: 'test', path: `test.${type}`, title: '测试', type }
      app = createApp(defineComponent({
        setup: () => () => h(DocumentContent, { document: doc, downloadEnabled: state.enabled }),
      }))
      app.mount(root)
      await nextTick()
      expect(root.querySelectorAll('a[download]')).toHaveLength(0)
      if (type === 'audio' || type === 'video') {
        expect(root.querySelector(type)?.getAttribute('controlslist')).toBe('nodownload')
      }

      state.enabled = true
      await nextTick()
      expect(root.querySelectorAll('a[download]')).toHaveLength(1)
      if (type === 'audio' || type === 'video') {
        expect(root.querySelector(type)?.hasAttribute('controlslist')).toBe(false)
      }

      state.enabled = false
      await nextTick()
      expect(root.querySelectorAll('a[download]')).toHaveLength(0)
    },
  )
})
