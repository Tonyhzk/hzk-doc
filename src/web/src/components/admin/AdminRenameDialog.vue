<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'

import { renameDocument } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import type { AdminEntry, AdminRenameResult } from '@/api/types'
import { directoryOf, joinPath } from '@/lib/navigation'

const props = defineProps<{
  open: boolean
  entry: AdminEntry | null
}>()

const emit = defineEmits<{
  close: []
  renamed: [result: AdminRenameResult, previousPath: string]
  notice: [message: string]
  authExpired: []
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
const name = ref('')
const busy = ref(false)
const error = ref('')

const directory = computed(() =>
  props.entry ? directoryOf(props.entry.path) : '',
)
const currentName = computed(() => props.entry?.name ?? '')
const targetPath = computed(() => {
  const value = name.value.trim()
  if (!value) {
    return joinPath(directory.value, '…')
  }
  return joinPath(directory.value, value)
})
const changed = computed(
  () => name.value.trim() !== '' && name.value.trim() !== currentName.value,
)

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      name.value = props.entry?.name ?? ''
      error.value = ''
      busy.value = false
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      nameInput.value?.focus()
      nameInput.value?.select()
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
  if (busy.value || !props.entry) {
    return
  }
  const value = name.value.trim()
  if (!value) {
    error.value = '请输入新名称。'
    return
  }
  if (
    value.includes('/') ||
    value.includes('\\') ||
    value.includes(':') ||
    value.startsWith('.')
  ) {
    error.value = '名称不能包含斜杠或冒号，也不能以点开头。'
    return
  }
  if (value === currentName.value) {
    error.value = '名称没有变化。'
    return
  }

  const previousPath = props.entry.path
  busy.value = true
  error.value = ''

  try {
    const result = await renameDocument(previousPath, value)
    emit('notice', '名称已更新。')
    emit('renamed', result, previousPath)
    emit('close')
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '改名失败，请稍后重试。'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="form-dialog"
    aria-labelledby="rename-title"
    @cancel.prevent="close"
    @close="emit('close')"
    @click="onBackdrop"
  >
    <form class="dialog-form" @submit.prevent="submit">
      <h2 id="rename-title">文件改名</h2>
      <p class="form-hint">
        将重命名 {{ directory || '文档根目录' }} 中的 {{ currentName }}。
      </p>

      <label class="field-label" for="rename-name">新名称</label>
      <input
        id="rename-name"
        ref="nameInput"
        v-model="name"
        class="text-input"
        placeholder="例如：使用指南.md"
        :disabled="busy"
        autocomplete="off"
      />

      <p class="path-preview">路径：{{ targetPath }}</p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>

      <div class="dialog-actions">
        <button type="button" class="button" :disabled="busy" @click="close">
          取消
        </button>
        <button
          type="submit"
          class="button button-primary"
          :disabled="busy || !changed"
        >
          {{ busy ? '处理中' : '改名' }}
        </button>
      </div>
    </form>
  </dialog>
</template>
