<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue'
import { useRouter } from 'vue-router'

import { fetchSource, previewSource, saveDocument } from '@/api/admin'
import { AdminAuthError, ApiError } from '@/api/http'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { copyText } from '@/lib/clipboard'
import type { EditorSnapshot } from '@/lib/editor'
import {
  applyReload,
  applySaveConflict,
  applySaveSuccess,
  createEditorSnapshot,
  isEditorDirty,
} from '@/lib/editor'
import {
  extensionLabel,
  isHtmlPath,
  isMarkdownPath,
  isTextPath,
} from '@/lib/format'

const props = defineProps<{
  path: string
}>()

const emit = defineEmits<{
  close: []
  dirty: [value: boolean]
  saved: [path: string]
  notice: [message: string]
  authExpired: []
}>()

const router = useRouter()
const editorId = `source-${Math.random().toString(36).slice(2, 9)}`
const snapshot = ref<EditorSnapshot>(createEditorSnapshot('', ''))
const loading = ref(true)
const loadError = ref('')
const saving = ref(false)
const reloading = ref(false)
const saveError = ref('')
const previewHtml = ref('')
const previewLoading = ref(false)
const previewError = ref('')
const pane = ref<'source' | 'preview'>('source')
const confirmOpen = ref(false)
let previewTimer: ReturnType<typeof setTimeout> | null = null
let previewSequence = 0

const content = computed({
  get: () => snapshot.value.content,
  set: (value: string) => {
    snapshot.value = { ...snapshot.value, content: value }
  },
})
const dirty = computed(() => isEditorDirty(snapshot.value))
const fileName = computed(
  () => props.path.slice(props.path.lastIndexOf('/') + 1) || props.path,
)
const fileKind = computed(() => extensionLabel(props.path))
const isMarkdown = computed(() => isMarkdownPath(props.path))
const isHtml = computed(() => isHtmlPath(props.path))
const isText = computed(() => isTextPath(props.path))

watch(
  dirty,
  (value) => {
    emit('dirty', value)
  },
  { immediate: true },
)

watch(
  () => snapshot.value.content,
  () => {
    if (isMarkdown.value && !loading.value) {
      schedulePreview()
    }
  },
)

onMounted(() => {
  window.addEventListener('beforeunload', onBeforeUnload)
  void loadSource()
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
  if (previewTimer) {
    clearTimeout(previewTimer)
  }
  previewSequence += 1
  emit('dirty', false)
})

async function loadSource(): Promise<void> {
  loading.value = true
  loadError.value = ''
  saveError.value = ''
  try {
    const source = await fetchSource(props.path)
    snapshot.value = createEditorSnapshot(source.content, source.revision)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      loadError.value = '登录已过期，请重新登录后重试。'
      emit('authExpired')
      return
    }
    loadError.value =
      cause instanceof Error ? cause.message : '文档加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function schedulePreview(immediate = false): void {
  if (previewTimer) {
    clearTimeout(previewTimer)
    previewTimer = null
  }
  const sequence = ++previewSequence
  previewLoading.value = true
  previewError.value = ''
  if (immediate) {
    void loadPreview(sequence)
    return
  }
  previewTimer = setTimeout(() => {
    previewTimer = null
    void loadPreview(sequence)
  }, 360)
}

async function loadPreview(sequence: number): Promise<void> {
  try {
    const result = await previewSource(props.path, snapshot.value.content)
    if (sequence !== previewSequence) {
      return
    }
    previewHtml.value = result.content
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    if (sequence !== previewSequence) {
      return
    }
    previewError.value =
      cause instanceof Error ? cause.message : '预览生成失败。'
  } finally {
    if (sequence === previewSequence) {
      previewLoading.value = false
    }
  }
}

async function save(): Promise<void> {
  if (saving.value || !dirty.value || snapshot.value.conflict) {
    return
  }
  saving.value = true
  saveError.value = ''
  const contentAtSave = snapshot.value.content

  try {
    const result = await saveDocument(
      props.path,
      contentAtSave,
      snapshot.value.revision,
    )
    const current = snapshot.value
    const applied = applySaveSuccess(
      { ...current, content: contentAtSave },
      result.revision,
    )
    snapshot.value = {
      ...current,
      savedContent: applied.savedContent,
      revision: applied.revision,
      conflict: false,
    }
    emit('notice', '已保存。')
    emit('saved', props.path)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    if (cause instanceof ApiError && cause.status === 409) {
      snapshot.value = applySaveConflict(snapshot.value)
      emit('notice', '文件已在别处修改，当前文本已保留。')
      return
    }
    saveError.value =
      cause instanceof Error ? cause.message : '保存失败，请稍后重试。'
  } finally {
    saving.value = false
  }
}

async function reloadSource(): Promise<void> {
  if (reloading.value) {
    return
  }
  reloading.value = true
  saveError.value = ''
  try {
    const source = await fetchSource(props.path)
    snapshot.value = applyReload(snapshot.value, source.content, source.revision)
    emit('notice', '已重新加载文件。')
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    saveError.value =
      cause instanceof Error ? cause.message : '重新加载失败，请稍后重试。'
  } finally {
    reloading.value = false
  }
}

async function copyLocalContent(): Promise<void> {
  const copied = await copyText(snapshot.value.content)
  emit('notice', copied ? '全文已复制。' : '复制失败，请手动选择文本。')
}

function requestClose(): void {
  if (dirty.value) {
    confirmOpen.value = true
    return
  }
  emit('close')
}

function confirmDiscard(): void {
  confirmOpen.value = false
  emit('close')
}

function openReader(): void {
  void router.push({ path: '/', query: { doc: props.path } })
}

function retryPreview(): void {
  schedulePreview(true)
}

function onBeforeUnload(event: BeforeUnloadEvent): void {
  if (isEditorDirty(snapshot.value)) {
    event.preventDefault()
    event.returnValue = ''
  }
}

function onEditorKeydown(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    void save()
    return
  }
  if (event.key === 'Tab') {
    event.preventDefault()
    void insertTab(event)
  }
}

async function insertTab(event: KeyboardEvent): Promise<void> {
  const element = event.target as HTMLTextAreaElement
  const start = element.selectionStart
  const end = element.selectionEnd
  const value = snapshot.value.content
  snapshot.value = {
    ...snapshot.value,
    content: `${value.slice(0, start)}  ${value.slice(end)}`,
  }
  await nextTick()
  element.setSelectionRange(start + 2, start + 2)
}
</script>

<template>
  <section class="admin-editor" aria-labelledby="editor-title">
    <header class="editor-head">
      <div class="editor-heading">
        <p class="editor-eyebrow">{{ fileKind }} · {{ path }}</p>
        <h1 id="editor-title">{{ fileName }}</h1>
      </div>
      <div class="editor-actions">
        <button type="button" class="button" @click="requestClose">
          返回
        </button>
        <button
          type="button"
          class="button button-primary"
          :disabled="saving || !dirty || snapshot.conflict"
          @click="save"
        >
          {{ saving ? '保存中' : '保存' }}
        </button>
        <button type="button" class="button button-quiet" @click="openReader">
          阅读
        </button>
      </div>
    </header>

    <div class="editor-statusbar">
      <span class="save-state" :class="{ dirty }">
        {{ dirty ? '未保存' : '已保存' }}
      </span>
      <span>Ctrl/⌘ + S 保存 · Tab 插入两个空格</span>
      <div class="pane-switch" role="group" aria-label="编辑视图">
        <button
          type="button"
          class="scope-button"
          :aria-pressed="pane === 'source'"
          @click="pane = 'source'"
        >
          源码
        </button>
        <button
          type="button"
          class="scope-button"
          :aria-pressed="pane === 'preview'"
          @click="pane = 'preview'"
        >
          预览
        </button>
      </div>
    </div>

    <div v-if="snapshot.conflict" class="banner banner-warning" role="alert">
      <div>
        <strong>文件已在别处修改</strong>
        <p>
          这次保存没有执行，当前编辑内容仍保留。重新加载会丢弃本地修改。
        </p>
      </div>
      <div class="banner-actions">
        <button
          type="button"
          class="button"
          :disabled="reloading"
          @click="reloadSource"
        >
          {{ reloading ? '加载中' : '重新加载' }}
        </button>
        <button type="button" class="button" @click="copyLocalContent">
          复制全文
        </button>
      </div>
    </div>

    <div v-if="saveError" class="banner banner-error" role="alert">
      <p>{{ saveError }}</p>
    </div>

    <div v-if="loading" class="admin-loading" role="status">
      正在读取文档…
    </div>

    <div v-else-if="loadError" class="state-panel" role="alert">
      <h2>文档加载失败</h2>
      <p>{{ loadError }}</p>
      <div class="state-actions">
        <button type="button" class="button button-primary" @click="loadSource">
          重试
        </button>
        <button type="button" class="button" @click="emit('close')">
          返回
        </button>
      </div>
    </div>

    <div
      v-else
      class="editor-split"
      :class="{ 'show-preview': pane === 'preview' }"
    >
      <section class="editor-pane editor-source">
        <label class="visually-hidden" :for="editorId">文档源码</label>
        <textarea
          :id="editorId"
          v-model="content"
          class="source-editor"
          spellcheck="false"
          @keydown="onEditorKeydown"
        ></textarea>
      </section>

      <section class="editor-pane editor-preview" aria-label="文档预览">
        <div class="preview-bar">
          <span>预览</span>
          <span v-if="previewLoading" class="preview-loading">更新中…</span>
          <span v-else-if="isHtml" class="preview-note">脚本已禁用</span>
        </div>
        <div class="preview-scroll">
          <div v-if="previewError" class="preview-error" role="alert">
            <p>{{ previewError }}</p>
            <button type="button" class="button" @click="retryPreview">
              重试
            </button>
          </div>
          <template v-else>
            <template v-if="isMarkdown">
              <div
                class="markdown-body preview-content"
                v-html="previewHtml"
              ></div>
              <p
                v-if="!previewHtml && !previewLoading"
                class="preview-note"
              >
                开始输入 Markdown，预览会显示在这里。
              </p>
            </template>
            <iframe
              v-else-if="isHtml"
              class="html-frame editor-html-frame"
              title="HTML 预览（脚本已禁用）"
              sandbox=""
              :srcdoc="content"
            ></iframe>
            <pre v-else-if="isText" class="text-body">{{ content }}</pre>
            <p v-else class="preview-note">此格式暂不支持分栏预览。</p>
          </template>
        </div>
      </section>
    </div>

    <ConfirmDialog
      :open="confirmOpen"
      title="放弃未保存的修改？"
      description="返回文件列表后，当前未保存的文本会丢失。"
      confirm-text="放弃"
      cancel-text="继续编辑"
      danger
      @confirm="confirmDiscard"
      @cancel="confirmOpen = false"
    />
  </section>
</template>
