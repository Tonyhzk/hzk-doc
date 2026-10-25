import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const init = vi.hoisted(() => vi.fn())
vi.mock('pptx-preview', () => ({ init }))

beforeEach(() => {
  vi.resetModules()
  init.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PPTX 默认文本样式', () => {
  it.each([
    { name: '未提供', source: undefined, expected: {} },
    { name: '空值', source: null, expected: {} },
    {
      name: '已有样式',
      source: { 'a:lvl1pPr': { 'a:defRPr': { sz: '1800' } } },
      expected: { 'a:lvl1pPr': { 'a:defRPr': { sz: '1800' } } },
    },
  ])('$name 时在主版解析前准备有效样式', async ({ source, expected }) => {
    let receive: ((event: MessageEvent) => void) | undefined
    const parent = { postMessage: vi.fn() }
    const stage = {
      replaceChildren: vi.fn(),
      getBoundingClientRect: () => ({ height: 450 }),
      scrollHeight: 450,
    }
    const pptx = {
      defaultTextStyleSource: undefined as unknown,
      _loadPresentation: vi.fn(async () => {
        pptx.defaultTextStyleSource = source
      }),
    }
    const previewer = {
      pptx: undefined as typeof pptx | undefined,
      preview: vi.fn(() => {
        previewer.pptx = pptx
        return Promise.resolve().then(async () => {
          await pptx._loadPresentation()
          Object.keys(pptx.defaultTextStyleSource as object)
        })
      }),
    }
    init.mockReturnValue(previewer)
    vi.stubGlobal('document', {
      getElementById: () => stage,
      documentElement: { clientWidth: 800 },
    })
    vi.stubGlobal('window', {
      __PPTX_CHANNEL__: 'test-channel',
      parent,
      addEventListener: (_type: string, listener: (event: MessageEvent) => void) => {
        receive = listener
      },
    })

    await import('../pptx-frame')
    expect(receive).toBeTypeOf('function')
    receive!({
      source: parent,
      data: { channel: 'test-channel', type: 'render', buffer: new ArrayBuffer(8) },
    } as unknown as MessageEvent)

    await vi.waitFor(() => {
      expect(parent.postMessage).toHaveBeenCalledWith(
        { channel: 'test-channel', type: 'rendered', height: 450 },
        '*',
      )
    })
    expect(parent.postMessage).toHaveBeenCalledTimes(1)
    expect(pptx.defaultTextStyleSource).toEqual(expected)
    if (source !== null && source !== undefined) {
      expect(pptx.defaultTextStyleSource).toBe(source)
    }
  })
})
