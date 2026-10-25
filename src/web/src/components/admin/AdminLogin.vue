<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'

const props = defineProps<{
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  submit: [password: string]
}>()

const password = ref('')
const input = ref<HTMLInputElement | null>(null)

watch(
  () => props.error,
  () => {
    input.value?.focus()
  },
)

onMounted(() => {
  input.value?.focus()
})

function submit(): void {
  if (!password.value || props.busy) {
    return
  }
  emit('submit', password.value)
}
</script>

<template>
  <section class="auth-card" aria-labelledby="login-title">
    <p class="auth-kicker">站点管理</p>
    <h1 id="login-title">管理员登录</h1>
    <p class="auth-description">
      输入管理密码后，可以新建、编辑、上传和排序文档。
    </p>

    <form class="auth-form" @submit.prevent="submit">
      <label class="field-label" for="admin-password">密码</label>
      <input
        id="admin-password"
        ref="input"
        v-model="password"
        class="text-input"
        type="password"
        autocomplete="current-password"
        :disabled="busy"
      />
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <button
        type="submit"
        class="button button-primary auth-submit"
        :disabled="busy || !password"
      >
        {{ busy ? '登录中' : '登录' }}
      </button>
    </form>

    <RouterLink class="auth-back" to="/">返回阅读</RouterLink>
  </section>
</template>
