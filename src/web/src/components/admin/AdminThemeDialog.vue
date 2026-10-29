<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

import { saveDownloadEnabled, saveSiteName, saveSiteTheme } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import type { SiteTheme } from '@/api/types'

const props = defineProps<{
  open: boolean
  name: string
  theme: SiteTheme
  downloadEnabled: boolean
}>()

const emit = defineEmits<{
  close: []
  nameSaved: [name: string]
  downloadSaved: [downloadEnabled: boolean]
  saved: [theme: SiteTheme]
  authExpired: []
}>()

const themes: { value: SiteTheme; name: string }[] = [
  { value: 'classic', name: '经典' },
  { value: 'forest', name: '青岚' },
  { value: 'mono', name: '黑白' },
]
const dialog = ref<HTMLDialogElement | null>(null)
const nameInput = ref<HTMLInputElement | null>(null)
const siteName = ref('')
const nameBusy = ref(false)
const nameError = ref('')
const downloadSelection = ref(false)
const downloadBusy = ref(false)
const downloadError = ref('')
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
      siteName.value = props.name
      nameError.value = ''
      downloadSelection.value = props.downloadEnabled
      downloadError.value = ''
      selection.value = props.theme
      error.value = ''
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
  if (!busy.value && !nameBusy.value && !downloadBusy.value) {
    emit('close')
  }
}

async function submitName(): Promise<void> {
  if (busy.value || nameBusy.value || downloadBusy.value) {
    return
  }
  const name = siteName.value.trim()
  if (!name || Array.from(name).length > 120) {
    nameError.value = '站点名称需为 1 到 120 个字符。'
    return
  }
  nameBusy.value = true
  nameError.value = ''
  try {
    const result = await saveSiteName(name)
    siteName.value = result.name
    emit('nameSaved', result.name)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
    } else {
      nameError.value = cause instanceof Error ? cause.message : '保存失败，请稍后重试。'
    }
  } finally {
    nameBusy.value = false
  }
}

async function submitDownload(): Promise<void> {
  if (busy.value || nameBusy.value || downloadBusy.value) {
    return
  }
  downloadBusy.value = true
  downloadError.value = ''
  try {
    const result = await saveDownloadEnabled(downloadSelection.value)
    downloadSelection.value = result.downloadEnabled
    emit('downloadSaved', result.downloadEnabled)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      emit('close')
    } else {
      downloadError.value = cause instanceof Error ? cause.message : '保存失败，请稍后重试。'
    }
  } finally {
    downloadBusy.value = false
  }
}

async function submit(): Promise<void> {
  if (busy.value || nameBusy.value || downloadBusy.value) {
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
    <div class="dialog-body site-settings">
      <h2 id="theme-dialog-title">站点设置</h2>
      <form class="site-name-form" @submit.prevent="submitName">
        <label class="field-label" for="site-name">站点名称</label>
        <div class="site-name-controls">
          <input
            id="site-name"
            ref="nameInput"
            v-model="siteName"
            class="text-input"
            :disabled="busy || nameBusy || downloadBusy"
            autocomplete="off"
          />
          <button type="submit" class="button" :disabled="busy || nameBusy || downloadBusy">
            {{ nameBusy ? '保存中' : '保存名称' }}
          </button>
        </div>
        <p v-if="nameError" class="form-error" role="alert">{{ nameError }}</p>
      </form>
      <form class="site-download-form" @submit.prevent="submitDownload">
        <div class="site-download-controls">
          <label for="site-download">
            <input
              id="site-download"
              v-model="downloadSelection"
              type="checkbox"
              :disabled="busy || nameBusy || downloadBusy"
            />
            显示下载按钮
          </label>
          <button type="submit" class="button" :disabled="busy || nameBusy || downloadBusy">
            {{ downloadBusy ? '保存中' : '保存下载' }}
          </button>
        </div>
        <p v-if="downloadError" class="form-error" role="alert">{{ downloadError }}</p>
      </form>
      <form class="site-theme-form" @submit.prevent="submit">
        <p class="field-label">站点主题</p>
        <p class="form-hint">主题对所有访客生效。</p>
        <fieldset class="theme-options" :disabled="busy || nameBusy || downloadBusy">
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
          <button type="button" class="button" :disabled="busy || nameBusy || downloadBusy" @click="close">关闭</button>
          <button type="submit" class="button button-primary" :disabled="busy || nameBusy || downloadBusy">
            {{ busy ? '保存中' : '保存主题' }}
          </button>
        </div>
      </form>
    </div>
  </dialog>
</template>
