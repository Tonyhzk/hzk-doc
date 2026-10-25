import { onScopeDispose, ref } from 'vue'

export function useNotice(duration = 2600) {
  const message = ref('')
  let timer: ReturnType<typeof setTimeout> | null = null

  function notify(text: string): void {
    if (timer) {
      clearTimeout(timer)
    }
    message.value = text
    timer = setTimeout(() => {
      message.value = ''
      timer = null
    }, duration)
  }

  onScopeDispose(() => {
    if (timer) {
      clearTimeout(timer)
    }
  })

  return { message, notify }
}
