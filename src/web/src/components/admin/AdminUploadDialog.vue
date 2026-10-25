<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'

import { uploadFile } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import { formatBytes, isAllowedUpload, uploadAccept } from '@/lib/format'

const props = defineProps<{
  open: boolean
  directory: string
}>()

const emit = defineEmits<{
  close: []
  uploaded: [path: string]
  notice: [message: string]
  authExpired: []
}>()

const maxBytes = 64 * 1024 * 1024

const dialog = ref<HTMLDialogElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const file = ref<File | null>(null)
const progress = ref(0)
const busy = ref(false)
const error = ref('')
const resultPath = ref('')
let controller: AbortController | null = null

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      reset()
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      fileInput.value?.focus()
    } else if (element.open) {
      abortUpload()
      element.close()
    }
  },
  { immediate: true },
)

function reset(): void {
  file.value = null
  progress.value = 0
  error.value = ''
  resultPath.value = ''
  busy.value = false
  if (fileInput.value) {
    fileInput.value.value = ''
  }
}

function close(): void {
  if (busy.value) {
    abortUpload()
  }
  emit('close')
}

function abortUpload(): void {
  controller?.abort()
  controller = null
  busy.value = false
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    close()
  }
}

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const selected = input.files?.[0] ?? null
  file.value = selected
  error.value = ''
  resultPath.value = ''
  progress.value = 0
  if (!selected) {
    return
  }
  if (selected.size > maxBytes) {
    error.value = '单个文件不能超过 64 MiB。'
    file.value = null
    input.value = ''
    return
  }
  if (!isAllowedUpload(selected.name)) {
    error.value = '不支持这个文件类型。'
    file.value = null
    input.value = ''
  }
}

onBeforeUnmount(() => {
  abortUpload()
})

async function submit(): Promise<void> {
  if (busy.value || !file.value) {
    return
  }
  error.value = ''
  resultPath.value = ''
  progress.value = 0
  busy.value = true
  const upload = new AbortController()
  controller = upload

  try {
    const result = await uploadFile(
      file.value,
      props.directory,
      (percent) => {
        progress.value = percent
      },
      upload.signal,
    )
    resultPath.value = result.path
    emit('uploaded', result.path)
    emit('notice', '上传完成。')
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') {
      return
    }
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '上传失败，请稍后重试。'
  } finally {
    if (controller === upload) {
      controller = null
      busy.value = false
    }
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="form-dialog"
    aria-labelledby="upload-title"
    @cancel.prevent="close"
    @close="emit('close')"
    @click="onBackdrop"
  >
    <div class="dialog-form">
      <h2 id="upload-title">上传文件</h2>
      <p class="form-hint">
        上传到 {{ directory || '文档根目录' }}，单个文件不超过 64 MiB。
      </p>

      <label class="field-label" for="upload-file">选择文件</label>
      <input
        id="upload-file"
        ref="fileInput"
        class="file-input"
        type="file"
        :accept="uploadAccept"
        :disabled="busy"
        @change="onFileChange"
      />
      <p v-if="file" class="file-summary">
        {{ file.name }} · {{ formatBytes(file.size) }}
      </p>

      <div v-if="busy || progress > 0" class="upload-progress">
        <progress :value="progress" max="100">{{ progress }}%</progress>
        <span>{{ progress }}%</span>
      </div>

      <p v-if="resultPath" class="upload-result" role="status">
        已上传到 {{ resultPath }}
      </p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>

      <div class="dialog-actions">
        <button type="button" class="button" @click="close">
          {{ busy ? '取消' : '关闭' }}
        </button>
        <button
          type="button"
          class="button button-primary"
          :disabled="busy || !file || Boolean(resultPath)"
          @click="submit"
        >
          {{ busy ? '上传中' : '上传' }}
        </button>
      </div>
    </div>
  </dialog>
</template>
