<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

import { searchDocuments } from '@/api/docs'
import type { SearchHit, SearchScope } from '@/api/types'
import { parseSnippet } from '@/lib/search'

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  close: []
  select: [path: string]
}>()

type SearchStatus = 'idle' | 'loading' | 'ready' | 'error'

const dialog = ref<HTMLDialogElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const query = ref('')
const scope = ref<SearchScope>('title')
const results = ref<SearchHit[]>([])
const status = ref<SearchStatus>('idle')
const errorMessage = ref('')
const activeIndex = ref(0)
let debounceTimer: ReturnType<typeof setTimeout> | null = null
let controller: AbortController | null = null

const trimmedQuery = computed(() => query.value.trim())
const showResults = computed(
  () => status.value === 'ready' && results.value.length > 0,
)

watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) {
      return
    }
    if (open) {
      if (!element.open) {
        element.showModal()
      }
      await nextTick()
      input.value?.focus()
      if (trimmedQuery.value) {
        scheduleSearch()
      }
    } else {
      cancelPending()
      if (element.open) {
        element.close()
      }
    }
  },
  { immediate: true },
)

watch(query, () => {
  scheduleSearch()
})

watch(scope, () => {
  if (props.open && trimmedQuery.value) {
    if (debounceTimer) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    runSearch()
  }
})

function cancelPending(): void {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  controller?.abort()
  controller = null
}

function scheduleSearch(): void {
  cancelPending()
  if (!trimmedQuery.value) {
    results.value = []
    status.value = 'idle'
    errorMessage.value = ''
    return
  }
  status.value = 'loading'
  errorMessage.value = ''
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    runSearch()
  }, 260)
}

async function runSearch(): Promise<void> {
  const value = trimmedQuery.value
  if (!value) {
    return
  }
  controller?.abort()
  const current = new AbortController()
  controller = current
  status.value = 'loading'
  errorMessage.value = ''
  activeIndex.value = 0

  try {
    const hits = await searchDocuments({
      query: value,
      scope: scope.value,
      signal: current.signal,
    })
    if (current.signal.aborted) {
      return
    }
    results.value = hits
    status.value = 'ready'
    activeIndex.value = 0
  } catch (error) {
    if (current.signal.aborted) {
      return
    }
    if (error instanceof DOMException && error.name === 'AbortError') {
      return
    }
    results.value = []
    status.value = 'error'
    errorMessage.value =
      error instanceof Error ? error.message : '搜索失败，请稍后重试。'
  }
}

function close(): void {
  emit('close')
}

function onNativeClose(): void {
  cancelPending()
  emit('close')
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    close()
  }
}

function onScopeChange(value: SearchScope): void {
  scope.value = value
}

async function moveActive(delta: number): Promise<void> {
  if (results.value.length === 0) {
    return
  }
  const length = results.value.length
  activeIndex.value = (activeIndex.value + delta + length) % length
  await nextTick()
  document
    .getElementById(`search-option-${activeIndex.value}`)
    ?.scrollIntoView({ block: 'nearest' })
}

function onInputKeydown(event: KeyboardEvent): void {
  if (event.isComposing) {
    return
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    void moveActive(1)
    return
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    void moveActive(-1)
    return
  }
  if (event.key === 'Enter') {
    if (showResults.value) {
      event.preventDefault()
      openHit(results.value[activeIndex.value])
    }
  }
}

function openHit(hit: SearchHit | undefined): void {
  if (!hit) {
    return
  }
  emit('select', hit.path)
  close()
}

function segments(hit: SearchHit) {
  return parseSnippet(hit.snippet)
}

onBeforeUnmount(() => {
  cancelPending()
})
</script>

<template>
  <dialog
    ref="dialog"
    class="search-dialog"
    aria-labelledby="search-title"
    @close="onNativeClose"
    @click="onBackdrop"
  >
    <div class="search-panel">
      <div class="search-head">
        <h2 id="search-title" class="visually-hidden">搜索文档</h2>
        <input
          ref="input"
          v-model="query"
          class="search-input"
          type="search"
          maxlength="100"
          placeholder="搜索文档标题或内容"
          autocomplete="off"
          spellcheck="false"
          role="combobox"
          aria-autocomplete="list"
          aria-label="搜索文档"
          aria-controls="search-results"
          aria-haspopup="listbox"
          :aria-expanded="showResults"
          :aria-activedescendant="
            showResults ? `search-option-${activeIndex}` : undefined
          "
          @keydown="onInputKeydown"
        />
        <button type="button" class="button button-quiet" @click="close">
          关闭
        </button>
      </div>

      <div class="search-controls">
        <div class="scope-switch" role="group" aria-label="搜索范围">
          <button
            type="button"
            class="scope-button"
            :aria-pressed="scope === 'title'"
            @click="onScopeChange('title')"
          >
            标题
          </button>
          <button
            type="button"
            class="scope-button"
            :aria-pressed="scope === 'full'"
            @click="onScopeChange('full')"
          >
            全文
          </button>
        </div>
        <p class="search-hint">
          {{ scope === 'title' ? '只搜索文档标题' : '搜索标题与正文' }}
        </p>
      </div>

      <div class="search-body">
        <p v-if="status === 'idle'" class="search-state">
          输入关键词开始搜索，支持标题与正文。
        </p>

        <div v-else-if="status === 'loading'" class="search-state" role="status">
          <span class="loading-line"></span>
          <span class="loading-line short"></span>
          <span class="loading-line"></span>
          <span class="visually-hidden">正在搜索</span>
        </div>

        <div v-else-if="status === 'error'" class="search-state" role="alert">
          <p>{{ errorMessage }}</p>
          <button
            type="button"
            class="button button-primary"
            @click="runSearch"
          >
            重试
          </button>
        </div>

        <p v-else-if="results.length === 0" class="search-state">
          没有找到相关文档。试试更短的关键词，或切换搜索范围。
        </p>

        <div
          v-else
          id="search-results"
          class="search-results"
          role="listbox"
          aria-label="搜索结果"
        >
          <button
            v-for="(hit, index) in results"
            :id="`search-option-${index}`"
            :key="hit.path"
            type="button"
            class="search-hit"
            role="option"
            :aria-selected="index === activeIndex"
            :class="{ active: index === activeIndex }"
            @mouseenter="activeIndex = index"
            @click="openHit(hit)"
          >
            <span class="hit-head">
              <span class="hit-title">{{ hit.title }}</span>
              <span class="hit-path">
                {{ hit.directory || '根目录' }}
                <span v-if="hit.extension">
                  · {{ hit.extension.toUpperCase() }}
                </span>
              </span>
            </span>
            <span v-if="hit.snippet" class="hit-snippet">
              <template
                v-for="(segment, segmentIndex) in segments(hit)"
                :key="segmentIndex"
              >
                <mark v-if="segment.match">{{ segment.text }}</mark>
                <template v-else>{{ segment.text }}</template>
              </template>
            </span>
          </button>
        </div>
      </div>

      <div class="search-footer">
        <span>↑↓ 选择</span>
        <span>Enter 打开</span>
        <span>Esc 关闭</span>
      </div>
    </div>
  </dialog>
</template>
