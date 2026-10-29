<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

import { saveSiteTheme } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import type { SiteTheme } from '@/api/types'

const props = defineProps<{
  open: boolean
  theme: SiteTheme
}>()

const emit = defineEmits<{
  close: []
  saved: [theme: SiteTheme]
  authExpired: []
}>()

const themes: { value: SiteTheme; name: string }[] = [
  { value: 'classic', name: '经典' },
  { value: 'forest', name: '青岚' },
  { value: 'mono', name: '黑白' },
]
const dialog = ref<HTMLDialogElement | null>(null)
const selection = ref<SiteTheme>('classic')
const busy = ref(false)
const error = ref('')

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      selection.value = props.theme
      error.value = ''
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      element.querySelector<HTMLInputElement>('input:checked')?.focus()
    } else if (element.open) {
      element.close()
    }
  },
  { immediate: true },
)

function close(): void {
  if (!busy.value) {
    emit('close')
  }
}

async function submit(): Promise<void> {
  if (busy.value) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    const result = await saveSiteTheme(selection.value)
    emit('saved', result.theme)
    emit('close')
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
    } else {
      error.value = cause instanceof Error ? cause.message : '保存失败，请稍后重试。'
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="form-dialog theme-dialog"
    aria-labelledby="theme-dialog-title"
    @cancel.prevent="close"
    @click="($event.target === dialog) && close()"
  >
    <form class="dialog-form" @submit.prevent="submit">
      <h2 id="theme-dialog-title">站点主题</h2>
      <p class="form-hint">主题对所有访客生效。</p>
      <fieldset class="theme-options" :disabled="busy">
        <legend class="visually-hidden">选择主题</legend>
        <label
          v-for="theme in themes"
          :key="theme.value"
          class="theme-option"
          :class="{ selected: selection === theme.value }"
        >
          <input v-model="selection" type="radio" name="site-theme" :value="theme.value" />
          <span class="theme-preview" :class="`theme-preview-${theme.value}`" aria-hidden="true">
            <span class="theme-preview-light"><b>Aa</b><i></i><i></i></span>
            <span class="theme-preview-dark"><b>Aa</b><i></i><i></i></span>
          </span>
          <span class="theme-option-name">{{ theme.name }}</span>
        </label>
      </fieldset>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="dialog-actions">
        <button type="button" class="button" :disabled="busy" @click="close">取消</button>
        <button type="submit" class="button button-primary" :disabled="busy">
          {{ busy ? '保存中' : '保存' }}
        </button>
      </div>
    </form>
  </dialog>
</template>
