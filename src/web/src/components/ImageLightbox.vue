<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

const props = defineProps<{
  open: boolean
  src: string
  alt: string
}>()

const emit = defineEmits<{
  close: []
}>()

const dialog = ref<HTMLDialogElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)

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
      closeButton.value?.focus()
    } else if (element.open) {
      element.close()
    }
  },
  { immediate: true },
)

function close(): void {
  emit('close')
}

function onBackdrop(event: MouseEvent): void {
  if (event.target === dialog.value) {
    close()
  }
}
</script>

<template>
  <dialog
    ref="dialog"
    class="lightbox"
    aria-label="图片预览"
    @close="close"
    @click="onBackdrop"
  >
    <div class="lightbox-inner">
      <img v-if="src" :src="src" :alt="alt" />
      <button
        ref="closeButton"
        type="button"
        class="button lightbox-close"
        @click="close"
      >
        关闭
      </button>
    </div>
  </dialog>
</template>
