<script setup lang="ts">
import DOMPurify from 'dompurify'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

import { documentFileUrl } from '@/api/docs'
import type { DocumentData } from '@/api/types'
import { kindLabel } from '@/lib/format'
import {
  renderPptxSandbox,
  type PptxSandboxHandle,
} from '@/lib/pptxSandbox'

const props = defineProps<{
  document: DocumentData
}>()

interface SheetRow {
  cells: string[]
}

interface SheetData {
  name: string
  rows: SheetRow[]
  truncated: boolean
}

const loading = ref(true)
const error = ref('')
const docxHtml = ref('')
const sheets = ref<SheetData[]>([])
const pptxStage = ref<HTMLDivElement | null>(null)
let controller = new AbortController()
let pptxHandle: PptxSandboxHandle | null = null

const downloadHref = computed(() =>
  documentFileUrl(props.document.id, true),
)
const typeLabel = computed(() => kindLabel(props.document.type))

onMounted(() => {
  void load()
})

onBeforeUnmount(() => {
  controller.abort()
  cleanupPptx()
})

async function load(): Promise<void> {
  controller.abort()
  controller = new AbortController()
  const signal = controller.signal

  cleanupPptx()
  loading.value = true
  error.value = ''
  docxHtml.value = ''
  sheets.value = []

  try {
    const response = await fetch(documentFileUrl(props.document.id), { signal })
    if (!response.ok) {
      throw new Error(`原文件读取失败（${response.status}）。`)
    }
    const buffer = await response.arrayBuffer()
    if (signal.aborted) {
      return
    }

    if (props.document.type === 'docx') {
      await renderDocx(buffer)
    } else if (props.document.type === 'xlsx') {
      await renderXlsx(buffer)
    } else if (props.document.type === 'pptx') {
      await renderPptx(buffer, signal)
    } else {
      throw new Error('暂不支持预览此格式。')
    }
  } catch (cause) {
    if (signal.aborted) {
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '文档解析失败，请下载原文件查看。'
  } finally {
    if (!signal.aborted) {
      loading.value = false
    }
  }
}

async function renderDocx(buffer: ArrayBuffer): Promise<void> {
  const module = await import('mammoth/mammoth.browser')
  const mammoth = module.default
  const result = await mammoth.convertToHtml({ arrayBuffer: buffer })
  docxHtml.value = DOMPurify.sanitize(result.value, {
    USE_PROFILES: { html: true },
    ADD_DATA_URI_TAGS: ['img'],
    ADD_ATTR: ['target'],
  })
}

async function renderXlsx(buffer: ArrayBuffer): Promise<void> {
  const module = await import('exceljs')
  const ExcelJS =
    (module as unknown as { default?: typeof module }).default ?? module
  const workbook = new ExcelJS.Workbook()
  await workbook.xlsx.load(buffer)
  sheets.value = workbook.worksheets.map((worksheet) => {
    const maxRows = Math.min(worksheet.rowCount || 0, 200)
    const maxColumns = Math.min(Math.max(worksheet.columnCount || 1, 1), 30)
    const rows: SheetRow[] = []

    for (let rowIndex = 1; rowIndex <= maxRows; rowIndex += 1) {
      const row = worksheet.getRow(rowIndex)
      const cells: string[] = []
      for (let columnIndex = 1; columnIndex <= maxColumns; columnIndex += 1) {
        cells.push(cellText(row.getCell(columnIndex).value))
      }
      while (cells.length > 1 && cells[cells.length - 1] === '') {
        cells.pop()
      }
      if (cells.some((value) => value !== '')) {
        rows.push({ cells })
      }
    }

    return {
      name: worksheet.name,
      rows,
      truncated:
        (worksheet.rowCount || 0) > maxRows ||
        (worksheet.columnCount || 0) > maxColumns,
    }
  })
}

async function renderPptx(
  buffer: ArrayBuffer,
  signal: AbortSignal,
): Promise<void> {
  await nextTick()
  const container = pptxStage.value
  if (!container) {
    throw new Error('预览容器初始化失败。')
  }

  const frame = await import('../../build/pptx-frame.js?raw')
  if (signal.aborted) {
    return
  }

  const handle = await renderPptxSandbox({
    container,
    script: frame.default,
    buffer,
    title: props.document.title,
    signal,
  })
  if (signal.aborted) {
    handle.cleanup()
    return
  }
  pptxHandle = handle
}

function cleanupPptx(): void {
  pptxHandle?.cleanup()
  pptxHandle = null
}

function cellText(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }
  if (value instanceof Date) {
    return value.toLocaleString('zh-CN')
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'object') {
    const record = value as Record<string, unknown>
    if (Array.isArray(record.richText)) {
      return record.richText
        .map((part) =>
          typeof part === 'object' && part !== null
            ? String((part as { text?: unknown }).text ?? '')
            : '',
        )
        .join('')
    }
    if (typeof record.text === 'string') {
      return record.text
    }
    if (record.result !== undefined && record.result !== null) {
      return cellText(record.result)
    }
    if (typeof record.hyperlink === 'string') {
      return record.hyperlink
    }
    if (typeof record.error === 'string') {
      return '#错误'
    }
    if ('formula' in record) {
      return ''
    }
  }
  return ''
}
</script>

<template>
  <section class="office-preview" :aria-label="`${typeLabel}预览`">
    <div class="office-toolbar">
      <span class="office-type">{{ typeLabel }}</span>
      <a class="button button-quiet" :href="downloadHref" download>
        下载
      </a>
    </div>

    <div v-if="loading" class="preview-status" role="status">
      正在解析文档，较大的文件可能需要几秒。
    </div>

    <div v-if="error" class="preview-error" role="alert">
      <p>{{ error }}</p>
      <button type="button" class="button" @click="load">重试</button>
    </div>

    <div
      v-show="document.type === 'docx' && docxHtml && !error"
      class="docx-body markdown-body"
      v-html="docxHtml"
    ></div>

    <div v-if="document.type === 'xlsx' && sheets.length && !error" class="xlsx-body">
      <section v-for="sheet in sheets" :key="sheet.name" class="xlsx-sheet">
        <h3>{{ sheet.name }}</h3>
        <p v-if="sheet.rows.length === 0" class="preview-note">
          这个工作表没有可显示的内容。
        </p>
        <div v-else class="table-scroll">
          <table class="xlsx-table">
            <thead>
              <tr>
                <th v-for="(cell, index) in sheet.rows[0]?.cells" :key="index">
                  {{ cell }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, rowIndex) in sheet.rows.slice(1)" :key="rowIndex">
                <td v-for="(cell, cellIndex) in row.cells" :key="cellIndex">
                  {{ cell }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="sheet.truncated" class="preview-note">
          仅显示前 200 行、30 列，完整内容请下载原文件。
        </p>
      </section>
    </div>

    <div
      v-show="document.type === 'pptx' && !error"
      ref="pptxStage"
      class="pptx-stage"
      :class="{ 'is-loading': loading }"
    ></div>

    <p v-if="!error" class="preview-note">
      预览仅保留文字、表格与基础结构，复杂排版可能与原文件不同。
    </p>
  </section>
</template>
