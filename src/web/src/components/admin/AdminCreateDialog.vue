<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'

import { createDocument, createFolder } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import {
  extensionOf,
  isEditablePath,
  isMarkdownPath,
} from '@/lib/format'
import { joinPath } from '@/lib/navigation'

const props = defineProps<{
  open: boolean
  kind: 'document' | 'folder'
  directory: string
}>()

const emit = defineEmits<{
  close: []
  created: [path: string]
  notice: [message: string]
  authExpired: []
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
const name = ref('')
const content = ref('')
const busy = ref(false)
const error = ref('')

const title = computed(() =>
  props.kind === 'document' ? '新建文档' : '新建目录',
)
const targetPath = computed(() => {
  const value = name.value.trim()
  if (!value) {
    return joinPath(props.directory, '…')
  }
  if (props.kind === 'document' && !extensionOf(value)) {
    return joinPath(props.directory, `${value}.md`)
  }
  return joinPath(props.directory, value)
})

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      name.value = ''
      content.value = ''
      error.value = ''
      busy.value = false
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      nameInput.value?.focus()
    } else if (element.open) {
      element.close()
    }
  },
  { immediate: true },
)

function close(): void {
  if (busy.value) {
    return
  }
  emit('close')
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    close()
  }
}

async function submit(): Promise<void> {
  if (busy.value) {
    return
  }
  const rawName = name.value.trim()
  if (!rawName) {
    error.value = '请输入名称。'
    return
  }
  if (
    rawName.includes('/') ||
    rawName.includes('\\') ||
    rawName.includes(':') ||
    rawName.startsWith('.')
  ) {
    error.value = '名称不能包含斜杠或冒号，也不能以点开头。'
    return
  }

  let target = rawName
  if (props.kind === 'document') {
    if (!extensionOf(target)) {
      target = `${target}.md`
    }
    if (!isEditablePath(target)) {
      error.value = '只能新建 Markdown、HTML 或文本文件。'
      return
    }
  }

  const path = joinPath(props.directory, target)
  busy.value = true
  error.value = ''

  try {
    if (props.kind === 'document') {
      const initialContent =
        content.value ||
        (isMarkdownPath(target) ? `# ${target.replace(/\.[^.]+$/, '')}\n\n` : '')
      await createDocument(path, initialContent)
      emit('notice', '文档已创建。')
    } else {
      await createFolder(path)
      emit('notice', '目录已创建。')
    }
    emit('created', path)
    emit('close')
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '创建失败，请稍后重试。'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="form-dialog"
    :aria-labelledby="`create-${kind}-title`"
    @cancel.prevent="close"
    @close="emit('close')"
    @click="onBackdrop"
  >
    <form class="dialog-form" @submit.prevent="submit">
      <h2 :id="`create-${kind}-title`">{{ title }}</h2>
      <p class="form-hint">将创建在 {{ directory || '文档根目录' }}。</p>

      <label class="field-label" for="create-name">名称</label>
      <input
        id="create-name"
        ref="nameInput"
        v-model="name"
        class="text-input"
        :placeholder="
          kind === 'document' ? '例如：使用指南.md' : '例如：参考资料'
        "
        :disabled="busy"
        autocomplete="off"
      />

      <template v-if="kind === 'document'">
        <label class="field-label" for="create-content">初始内容</label>
        <textarea
          id="create-content"
          v-model="content"
          class="text-area initial-content"
          rows="6"
          :disabled="busy"
          placeholder="可以留空，创建后继续编辑"
        ></textarea>
      </template>

      <p class="path-preview">路径：{{ targetPath }}</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>

      <div class="dialog-actions">
        <button type="button" class="button" :disabled="busy" @click="close">
          取消
        </button>
        <button
          type="submit"
          class="button button-primary"
          :disabled="busy || !name.trim()"
        >
          {{ busy ? '创建中' : '创建' }}
        </button>
      </div>
    </form>
  </dialog>
</template>
