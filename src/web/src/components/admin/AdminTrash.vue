<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { emptyTrash, fetchTrash, purgeTrash, restoreTrash } from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import type { TrashEntry } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { formatBytes, formatDate } from '@/lib/format'

const emit = defineEmits<{
  notice: [message: string]
  authExpired: []
  back: []
}>()

const entries = ref<TrashEntry[]>([])
const totalSize = ref(0)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const selected = ref<string[]>([])

type PendingAction = 'restore' | 'purge' | 'empty'
const pendingAction = ref<PendingAction | null>(null)

const selectedEntries = computed(() =>
  entries.value.filter((entry) => selected.value.includes(entry.id)),
)
const selectedSize = computed(() =>
  selectedEntries.value.reduce((total, entry) => total + entry.size, 0),
)
const allSelected = computed(
  () => entries.value.length > 0 && selected.value.length === entries.value.length,
)

onMounted(() => {
  void load()
})

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const data = await fetchTrash()
    entries.value = data.entries
    totalSize.value = data.size
    selected.value = selected.value.filter((id) =>
      data.entries.some((entry) => entry.id === id),
    )
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '回收站加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function toggle(id: string): void {
  if (selected.value.includes(id)) {
    selected.value = selected.value.filter((item) => item !== id)
    return
  }
  selected.value = [...selected.value, id]
}

function toggleAll(): void {
  selected.value = allSelected.value ? [] : entries.value.map((entry) => entry.id)
}

const confirmOpen = computed(() => pendingAction.value !== null)

const confirmTitle = computed(() => {
  switch (pendingAction.value) {
    case 'restore':
      return '还原所选内容？'
    case 'purge':
      return '彻底删除所选条目？'
    case 'empty':
      return '清空回收站？'
    default:
      return ''
  }
})

const confirmDescription = computed(() => {
  switch (pendingAction.value) {
    case 'restore':
      return `将把 ${selected.value.length} 个条目还原到删除前的位置；原位置已有同名内容时会取消还原。`
    case 'purge':
      return `将彻底删除 ${selected.value.length} 个条目（${formatBytes(selectedSize.value)}），此操作无法撤销。`
    case 'empty':
      return `将彻底删除回收站中的全部 ${entries.value.length} 个条目（${formatBytes(totalSize.value)}），此操作无法撤销。`
    default:
      return ''
  }
})

const confirmText = computed(() => {
  if (busy.value) {
    return '处理中'
  }
  switch (pendingAction.value) {
    case 'restore':
      return '还原'
    case 'purge':
    case 'empty':
      return '彻底删除'
    default:
      return '确认'
  }
})

function requestRestore(): void {
  if (selected.value.length === 0) {
    return
  }
  pendingAction.value = 'restore'
}

function requestPurge(): void {
  if (selected.value.length === 0) {
    return
  }
  pendingAction.value = 'purge'
}

function requestEmpty(): void {
  if (entries.value.length === 0) {
    return
  }
  pendingAction.value = 'empty'
}

function cancelConfirm(): void {
  if (busy.value) {
    return
  }
  pendingAction.value = null
}

async function runPending(): Promise<void> {
  const action = pendingAction.value
  if (!action || busy.value) {
    return
  }
  busy.value = true
  error.value = ''
  try {
    if (action === 'empty') {
      const result = await emptyTrash()
      emit('notice', `已清空回收站，彻底删除 ${result.count} 个条目。`)
    } else if (action === 'purge') {
      const result = await purgeTrash(selected.value)
      emit('notice', `已彻底删除 ${result.count} 个条目。`)
    } else {
      const result = await restoreTrash(selected.value)
      emit('notice', `已还原 ${result.count} 个条目。`)
    }
    pendingAction.value = null
    selected.value = []
    await load()
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    pendingAction.value = null
    error.value =
      cause instanceof Error ? cause.message : '操作失败，请稍后重试。'
  } finally {
    busy.value = false
  }
}

function entryTitle(entry: TrashEntry): string {
  const first = entry.items[0]
  if (!first) {
    return '未知条目'
  }
  return entry.items.length > 1
    ? `${first.path} 等 ${entry.items.length} 项`
    : first.path
}

function itemKind(item: TrashEntry['items'][number]): string {
  return item.type === 'folder' ? '目录' : '文件'
}

function entryMeta(entry: TrashEntry): string {
  const parts = [formatDate(entry.deletedAt), formatBytes(entry.size)]
  return parts.filter(Boolean).join(' · ')
}
</script>

<template>
  <section class="admin-browser" aria-labelledby="trash-title">
    <header class="admin-browser-head">
      <div>
        <h1 id="trash-title">回收站</h1>
        <p class="admin-description">
          删除的文档与素材会先保存在这里，清空回收站才会彻底删除。
        </p>
      </div>
      <div class="admin-toolbar">
        <button
          type="button"
          class="button button-quiet"
          @click="emit('back')"
        >
          返回文件管理
        </button>
        <button
          type="button"
          class="button"
          :disabled="loading || busy || selected.length === 0"
          @click="requestRestore"
        >
          还原所选
        </button>
        <button
          type="button"
          class="button"
          :disabled="loading || busy || selected.length === 0"
          @click="requestPurge"
        >
          删除所选
        </button>
        <button
          type="button"
          class="button button-danger"
          :disabled="loading || busy || entries.length === 0"
          @click="requestEmpty"
        >
          清空回收站
        </button>
      </div>
    </header>

    <div v-if="error" class="banner banner-error" role="alert">
      <p>{{ error }}</p>
      <button type="button" class="button" @click="load">重试</button>
    </div>

    <div v-if="loading" class="admin-loading" role="status">
      正在读取回收站…
    </div>

    <p v-else-if="entries.length === 0 && !error" class="admin-empty">
      回收站是空的。
    </p>

    <template v-else>
      <div class="trash-summary">
        <label class="trash-select-all">
          <input
            type="checkbox"
            :checked="allSelected"
            :disabled="busy"
            @change="toggleAll"
          />
          全选
        </label>
        <span class="entry-meta">
          共 {{ entries.length }} 个条目 · {{ formatBytes(totalSize) }}
          <template v-if="selected.length > 0">
            · 已选 {{ selected.length }} 个（{{ formatBytes(selectedSize) }}）
          </template>
        </span>
      </div>

      <ul class="entry-list">
        <li v-for="entry in entries" :key="entry.id" class="trash-row">
          <label class="trash-row-head">
            <input
              type="checkbox"
              :checked="selected.includes(entry.id)"
              :disabled="busy"
              @change="toggle(entry.id)"
            />
            <span class="entry-name">{{ entryTitle(entry) }}</span>
            <span class="entry-meta">{{ entryMeta(entry) }}</span>
          </label>
          <ul class="trash-items">
            <li
              v-for="item in entry.items"
              :key="item.path"
              class="trash-item"
            >
              <span class="entry-name">{{ item.path }}</span>
              <span class="entry-meta">
                {{ itemKind(item) }} · {{ formatBytes(item.size) }}
              </span>
            </li>
          </ul>
        </li>
      </ul>
    </template>

    <ConfirmDialog
      :open="confirmOpen"
      :title="confirmTitle"
      :description="confirmDescription"
      :confirm-text="confirmText"
      :danger="pendingAction !== 'restore'"
      @confirm="runPending"
      @cancel="cancelConfirm"
    />
  </section>
</template>
