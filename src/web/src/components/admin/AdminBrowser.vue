<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { deleteEntry, fetchEntries, saveOrder } from '@/api/admin'
import { fileUrl } from '@/api/docs'
import { AdminAuthError } from '@/api/http'
import type { AdminEntry } from '@/api/types'
import AdminCreateDialog from '@/components/admin/AdminCreateDialog.vue'
import AdminUploadDialog from '@/components/admin/AdminUploadDialog.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import {
  fileMark,
  formatBytes,
  formatDate,
  isAllowedUpload,
  isHtmlPath,
  isMarkdownPath,
  kindLabel,
} from '@/lib/format'
import { canMove, moveItemBy, orderNames } from '@/lib/order'
import { pathTrail } from '@/lib/tree'

const emit = defineEmits<{
  edit: [path: string]
  view: [path: string]
  notice: [message: string]
  authExpired: []
  openTrash: []
}>()

const entries = ref<AdminEntry[]>([])
const orderedEntries = ref<AdminEntry[]>([])
const directory = ref('')
const loading = ref(true)
const error = ref('')
const orderMode = ref(false)
const orderDirty = ref(false)
const orderSaving = ref(false)
const createOpen = ref(false)
const createKind = ref<'document' | 'folder'>('document')
const uploadOpen = ref(false)
const deleteTarget = ref<AdminEntry | null>(null)
const deleting = ref(false)

const breadcrumbs = computed(() => pathTrail(directory.value))
const displayEntries = computed(() =>
  orderMode.value ? orderedEntries.value : entries.value,
)
let loadSequence = 0

onMounted(() => {
  void load('')
})

async function load(path: string): Promise<void> {
  const sequence = ++loadSequence
  loading.value = true
  error.value = ''
  orderMode.value = false
  orderDirty.value = false
  try {
    const data = await fetchEntries(path)
    if (sequence !== loadSequence) {
      return
    }
    directory.value = data.directory
    entries.value = data.entries
    orderedEntries.value = data.entries.slice()
  } catch (cause) {
    if (sequence !== loadSequence) {
      return
    }
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '目录加载失败，请稍后重试。'
  } finally {
    if (sequence === loadSequence) {
      loading.value = false
    }
  }
}

async function refresh(): Promise<void> {
  await load(directory.value)
}

defineExpose({ refresh })

function openDirectory(path: string): void {
  if (orderMode.value) {
    return
  }
  void load(path)
}

function entryMeta(entry: AdminEntry): string {
  if (entry.type === 'folder') {
    return '目录'
  }
  const parts = [kindLabel(entry.kind), formatBytes(entry.size)]
  const date = formatDate(entry.modifiedAt)
  if (date) {
    parts.push(date)
  }
  return parts.filter(Boolean).join(' · ')
}

function canDownloadEntry(entry: AdminEntry): boolean {
  return (
    entry.type === 'file' &&
    isAllowedUpload(entry.path) &&
    !isMarkdownPath(entry.path) &&
    !isHtmlPath(entry.path)
  )
}

function startOrder(): void {
  if (entries.value.length < 2) {
    return
  }
  orderedEntries.value = entries.value.slice()
  orderMode.value = true
  orderDirty.value = false
}

function cancelOrder(): void {
  orderMode.value = false
  orderDirty.value = false
  orderedEntries.value = entries.value.slice()
}

function moveEntry(index: number, delta: number): void {
  if (!canMove(index, orderedEntries.value.length, delta)) {
    return
  }
  orderedEntries.value = moveItemBy(orderedEntries.value, index, delta)
  orderDirty.value = true
}

async function persistOrder(): Promise<void> {
  if (orderSaving.value || !orderDirty.value) {
    return
  }
  orderSaving.value = true
  error.value = ''
  try {
    await saveOrder(directory.value, orderNames(orderedEntries.value))
    emit('notice', '排序已保存。')
    await load(directory.value)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '排序保存失败，请稍后重试。'
  } finally {
    orderSaving.value = false
  }
}

function openCreate(kind: 'document' | 'folder'): void {
  createKind.value = kind
  createOpen.value = true
}

function onCreated(path: string): void {
  createOpen.value = false
  if (createKind.value === 'document') {
    emit('edit', path)
    return
  }
  void load(directory.value)
}

function onUploaded(): void {
  void load(directory.value)
}

function requestDelete(entry: AdminEntry): void {
  deleteTarget.value = entry
}

function cancelDelete(): void {
  deleteTarget.value = null
}

const deleteDescription = computed(() => {
  const target = deleteTarget.value
  if (!target) {
    return ''
  }
  if (target.type === 'folder') {
    return `“${target.name}”及其中的全部内容会移入回收站，只被这些文档引用的素材会一起移入。`
  }
  return `“${target.name}”会移入回收站，只被这份文档引用的素材会一起移入。`
})

async function confirmDelete(): Promise<void> {
  const target = deleteTarget.value
  if (!target || deleting.value) {
    return
  }
  deleting.value = true
  error.value = ''
  try {
    const result = await deleteEntry(target.path)
    deleteTarget.value = null
    const extra = result.items.length - 1
    emit(
      'notice',
      extra > 0
        ? `已移入回收站，包含 ${extra} 个关联素材。`
        : '已移入回收站。',
    )
    await load(directory.value)
  } catch (cause) {
    if (cause instanceof AdminAuthError) {
      emit('authExpired')
      return
    }
    error.value =
      cause instanceof Error ? cause.message : '删除失败，请稍后重试。'
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <section class="admin-browser" aria-labelledby="browser-title">
    <header class="admin-browser-head">
      <div>
        <h1 id="browser-title">文件管理</h1>
        <p class="admin-description">
          管理当前目录中的文档、子目录与素材文件。
        </p>
      </div>
      <div class="admin-toolbar">
        <template v-if="!orderMode">
          <button
            type="button"
            class="button button-primary"
            :disabled="loading"
            @click="openCreate('document')"
          >
            新建文档
          </button>
          <button
            type="button"
            class="button"
            :disabled="loading"
            @click="openCreate('folder')"
          >
            新建目录
          </button>
          <button
            type="button"
            class="button"
            :disabled="loading"
            @click="uploadOpen = true"
          >
            上传
          </button>
          <button
            type="button"
            class="button button-quiet order-start"
            :disabled="loading || entries.length < 2"
            @click="startOrder"
          >
            调整排序
          </button>
          <button
            type="button"
            class="button button-quiet"
            :disabled="loading"
            @click="emit('openTrash')"
          >
            回收站
          </button>
        </template>
        <template v-else>
          <button
            type="button"
            class="button button-primary"
            :disabled="orderSaving || !orderDirty"
            @click="persistOrder"
          >
            {{ orderSaving ? '保存中' : '保存排序' }}
          </button>
          <button type="button" class="button" @click="cancelOrder">
            取消
          </button>
        </template>
      </div>
    </header>

    <nav class="admin-breadcrumb" aria-label="目录路径">
      <button
        type="button"
        class="crumb-button"
        :disabled="orderMode"
        @click="openDirectory('')"
      >
        全部文档
      </button>
      <template v-for="(segment, index) in breadcrumbs" :key="segment.path">
        <span class="crumb-separator" aria-hidden="true">/</span>
        <button
          v-if="index < breadcrumbs.length - 1 && !orderMode"
          type="button"
          class="crumb-button"
          @click="openDirectory(segment.path)"
        >
          {{ segment.name }}
        </button>
        <span v-else class="crumb-current">
          {{ segment.name }}
        </span>
      </template>
    </nav>

    <p v-if="orderMode" class="order-hint" role="status">
      使用上移、下移调整当前目录的顺序，保存后刷新目录。
    </p>

    <div v-if="error" class="banner banner-error" role="alert">
      <p>{{ error }}</p>
      <button
        v-if="!orderMode"
        type="button"
        class="button"
        @click="load(directory)"
      >
        重试
      </button>
    </div>

    <div v-if="loading" class="admin-loading" role="status">
      正在读取目录…
    </div>

    <p v-else-if="displayEntries.length === 0 && !error" class="admin-empty">
      这个目录还是空的。可以新建文档、创建目录或上传文件。
    </p>

    <ul v-else-if="displayEntries.length > 0" class="entry-list">
      <li
        v-for="(entry, index) in displayEntries"
        :key="entry.path"
        class="entry-row"
        :class="{ 'is-folder': entry.type === 'folder' }"
      >
        <div class="entry-main">
          <span v-if="entry.type === 'file'" class="file-mark" aria-hidden="true">
            {{ fileMark(entry.kind, entry.path) }}
          </span>
          <button
            v-if="entry.type === 'folder'"
            type="button"
            class="entry-name entry-folder-name"
            :disabled="orderMode"
            @click="openDirectory(entry.path)"
          >
            {{ entry.name }}
          </button>
          <span v-else class="entry-name">{{ entry.name }}</span>
          <span class="entry-meta">{{ entryMeta(entry) }}</span>
        </div>

        <div class="entry-actions">
          <template v-if="orderMode">
            <button
              type="button"
              class="button button-quiet"
              :disabled="!canMove(index, displayEntries.length, -1)"
              :aria-label="`上移 ${entry.name}`"
              @click="moveEntry(index, -1)"
            >
              上移
            </button>
            <button
              type="button"
              class="button button-quiet"
              :disabled="!canMove(index, displayEntries.length, 1)"
              :aria-label="`下移 ${entry.name}`"
              @click="moveEntry(index, 1)"
            >
              下移
            </button>
          </template>
          <template v-else>
            <button
              v-if="entry.type === 'folder'"
              type="button"
              class="button button-quiet"
              :aria-label="`打开 ${entry.name}`"
              @click="openDirectory(entry.path)"
            >
              打开
            </button>
            <template v-else>
              <button
                v-if="entry.kind"
                type="button"
                class="button button-quiet"
                :aria-label="`查看 ${entry.name}`"
                @click="emit('view', entry.path)"
              >
                查看
              </button>
              <button
                v-if="entry.editable"
                type="button"
                class="button button-quiet"
                :aria-label="`编辑 ${entry.name}`"
                @click="emit('edit', entry.path)"
              >
                编辑
              </button>
              <a
                v-if="canDownloadEntry(entry)"
                class="button button-quiet"
                :aria-label="`下载 ${entry.name}`"
                :href="fileUrl(entry.path, true)"
                download
              >
                下载
              </a>
            </template>
            <button
              type="button"
              class="button button-quiet button-danger-text"
              :aria-label="`删除 ${entry.name}`"
              @click="requestDelete(entry)"
            >
              删除
            </button>
          </template>
        </div>
      </li>
    </ul>

    <AdminCreateDialog
      :open="createOpen"
      :kind="createKind"
      :directory="directory"
      @close="createOpen = false"
      @created="onCreated"
      @notice="emit('notice', $event)"
      @auth-expired="emit('authExpired')"
    />
    <AdminUploadDialog
      :open="uploadOpen"
      :directory="directory"
      @close="uploadOpen = false"
      @uploaded="onUploaded"
      @notice="emit('notice', $event)"
      @auth-expired="emit('authExpired')"
    />
    <ConfirmDialog
      :open="deleteTarget !== null"
      title="移入回收站？"
      :description="deleteDescription"
      :confirm-text="deleting ? '处理中' : '移入回收站'"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="cancelDelete"
    />
  </section>
</template>
