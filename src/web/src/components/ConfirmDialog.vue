<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    description?: string
    confirmText?: string
    cancelText?: string
    danger?: boolean
  }>(),
  {
    description: '',
    confirmText: '确认',
    cancelText: '取消',
    danger: false,
  },
)

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const cancelButton = ref<HTMLButtonElement | null>(null)
const confirmButton = ref<HTMLButtonElement | null>(null)
const titleId = `confirm-title-${Math.random().toString(36).slice(2, 8)}`

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
      ;(props.danger ? cancelButton.value : confirmButton.value)?.focus()
    } else if (element.open) {
      element.close()
    }
  },
  { immediate: true },
)

function cancel(): void {
  emit('cancel')
}

function confirm(): void {
  emit('confirm')
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    cancel()
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="confirm-dialog"
    :aria-labelledby="titleId"
    @cancel.prevent="cancel"
    @click="onBackdrop"
  >
    <div class="dialog-body">
      <h2 :id="titleId">{{ title }}</h2>
      <p v-if="description" class="dialog-description">{{ description }}</p>
      <div class="dialog-actions">
        <button
          ref="cancelButton"
          type="button"
          class="button"
          @click="cancel"
        >
          {{ cancelText }}
        </button>
        <button
          ref="confirmButton"
          type="button"
          class="button"
          :class="danger ? 'button-danger' : 'button-primary'"
          @click="confirm"
        >
          {{ confirmText }}
        </button>
      </div>
    </div>
  </dialog>
</template>
