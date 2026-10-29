<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import {
  onBeforeRouteLeave,
  useRouter,
  type RouteLocationRaw,
} from 'vue-router'

import {
  fetchSession,
  login,
  logout,
  setAdminToken,
} from '@/api/admin'
import { AdminAuthError } from '@/api/http'
import type { AdminRenameResult, SiteTheme } from '@/api/types'
import AdminBrowser from '@/components/admin/AdminBrowser.vue'
import AdminEditor from '@/components/admin/AdminEditor.vue'
import AdminLogin from '@/components/admin/AdminLogin.vue'
import AdminThemeDialog from '@/components/admin/AdminThemeDialog.vue'
import AdminTrash from '@/components/admin/AdminTrash.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import ToastNotice from '@/components/ToastNotice.vue'
import { useNotice } from '@/composables/useNotice'
import { readingLocation } from '@/lib/navigation'
import { useSiteStore } from '@/stores/site'

type Phase = 'loading' | 'error' | 'disabled' | 'login' | 'ready'

const router = useRouter()
const { site, loadSite, applySiteTheme } = useSiteStore()
const { message, notify } = useNotice()

const phase = ref<Phase>('loading')
const loadError = ref('')
const loginError = ref('')
const loginBusy = ref(false)
const logoutBusy = ref(false)
const workspaceMounted = ref(false)
const activePath = ref('')
const editorDirty = ref(false)
const trashOpen = ref(false)
const themeOpen = ref(false)
const browserRef = ref<InstanceType<typeof AdminBrowser> | null>(null)

const confirmOpen = ref(false)
const confirmTitle = ref('')
const confirmDescription = ref('')
const confirmText = ref('确认')
const confirmDanger = ref(false)
let confirmAction: (() => void) | null = null
let pendingRoute: RouteLocationRaw | null = null

onMounted(() => {
  document.title = `管理 · ${site.name}`
  void loadSite()
  void initialize()
})

watch(
  () => site.name,
  (name) => {
    document.title = `管理 · ${name}`
  },
)

onBeforeRouteLeave((to) => {
  if (!editorDirty.value || !workspaceMounted.value) {
    return true
  }
  if (confirmOpen.value) {
    return false
  }
  pendingRoute = { path: to.path, query: to.query, hash: to.hash }
  askConfirm(
    '放弃未保存的修改？',
    '离开管理页面后，当前编辑器中未保存的文本会丢失。',
    '放弃',
    discardAndNavigate,
    true,
  )
  return false
})

async function initialize(): Promise<void> {
  phase.value = 'loading'
  loadError.value = ''
  try {
    const data = await fetchSession()
    if (!data.enabled) {
      phase.value = 'disabled'
      return
    }
    if (!data.authenticated) {
      phase.value = 'login'
      return
    }
    workspaceMounted.value = true
    phase.value = 'ready'
  } catch (cause) {
    phase.value = 'error'
    loadError.value =
      cause instanceof Error ? cause.message : '管理界面加载失败，请稍后重试。'
  }
}

async function submitLogin(password: string): Promise<void> {
  if (loginBusy.value) {
    return
  }
  loginBusy.value = true
  loginError.value = ''
  try {
    await login(password)
    workspaceMounted.value = true
    phase.value = 'ready'
    notify('登录成功。')
    browserRef.value?.refresh()
  } catch (cause) {
    loginError.value =
      cause instanceof Error ? cause.message : '登录失败，请稍后重试。'
  } finally {
    loginBusy.value = false
  }
}

function handleAuthExpired(): void {
  themeOpen.value = false
  setAdminToken(undefined)
  loginError.value = '登录已过期，请重新登录。'
  phase.value = 'login'
}

function requestLogout(): void {
  if (editorDirty.value) {
    askConfirm(
      '放弃未保存的修改？',
      '退出登录后，当前编辑器中未保存的文本会丢失。',
      '退出',
      () => {
        void performLogout()
      },
      true,
    )
    return
  }
  void performLogout()
}

async function performLogout(): Promise<void> {
  if (logoutBusy.value) {
    return
  }
  logoutBusy.value = true
  try {
    await logout()
  } catch (cause) {
    if (!(cause instanceof AdminAuthError)) {
      notify(cause instanceof Error ? cause.message : '退出失败，请重试。')
      logoutBusy.value = false
      return
    }
  }
  setAdminToken(undefined)
  workspaceMounted.value = false
  activePath.value = ''
  editorDirty.value = false
  loginError.value = ''
  phase.value = 'login'
  logoutBusy.value = false
  notify('已退出登录。')
}

async function openTheme(): Promise<void> {
  if (await loadSite()) {
    themeOpen.value = true
  } else {
    notify('站点信息加载失败，请重试。')
  }
}

function onThemeSaved(theme: SiteTheme): void {
  applySiteTheme(theme)
  notify('主题已保存。')
}

function openEditor(path: string): void {
  editorDirty.value = false
  activePath.value = path
}

function closeEditor(): void {
  activePath.value = ''
  editorDirty.value = false
  void browserRef.value?.refresh()
}

function openTrash(): void {
  editorDirty.value = false
  activePath.value = ''
  trashOpen.value = true
}

function closeTrash(): void {
  trashOpen.value = false
  void browserRef.value?.refresh()
}

function viewDocument(id: string): void {
  void router.push(readingLocation(id))
}

function onRenamed(
  result: AdminRenameResult,
  previousPath: string,
): void {
  if (activePath.value === previousPath) {
    activePath.value = result.path
  }
}

function onSaved(): void {
  browserRef.value?.refresh()
}

function askConfirm(
  title: string,
  description: string,
  text: string,
  action: () => void,
  danger = false,
): void {
  confirmTitle.value = title
  confirmDescription.value = description
  confirmText.value = text
  confirmDanger.value = danger
  confirmAction = action
  confirmOpen.value = true
}

function confirmPending(): void {
  const action = confirmAction
  confirmAction = null
  confirmOpen.value = false
  action?.()
}

function cancelConfirm(): void {
  confirmAction = null
  confirmOpen.value = false
  pendingRoute = null
}

function discardAndNavigate(): void {
  const target = pendingRoute
  pendingRoute = null
  editorDirty.value = false
  if (target) {
    void router.push(target)
  }
}
</script>

<template>
  <div class="admin-page">
    <header class="site-header">
      <div class="header-inner">
        <RouterLink class="brand" to="/">
          {{ site.name }}
          <span class="brand-tag">管理</span>
        </RouterLink>
        <div class="header-actions">
          <ThemeToggle />
          <button
            v-if="workspaceMounted && phase === 'ready'"
            type="button"
            class="button button-quiet"
            @click="openTheme"
          >
            主题
          </button>
          <RouterLink class="button button-quiet" to="/">阅读</RouterLink>
          <button
            v-if="workspaceMounted && phase === 'ready'"
            type="button"
            class="button button-quiet"
            :disabled="logoutBusy"
            @click="requestLogout"
          >
            {{ logoutBusy ? '退出中' : '退出' }}
          </button>
        </div>
      </div>
    </header>

    <main id="main-content" class="admin-main" tabindex="-1">
      <div v-if="phase === 'loading'" class="admin-loading" role="status">
        正在检查登录状态…
      </div>

      <section v-else-if="phase === 'error'" class="state-panel" role="alert">
        <h1>管理界面加载失败</h1>
        <p>{{ loadError }}</p>
        <div class="state-actions">
          <button
            type="button"
            class="button button-primary"
            @click="initialize"
          >
            重试
          </button>
          <RouterLink class="button" to="/">返回阅读</RouterLink>
        </div>
      </section>

      <section v-else-if="phase === 'disabled'" class="state-panel">
        <h1>管理功能未启用</h1>
        <p>
          设置环境变量
          <code>HZK_DOC_ADMIN_PASSWORD</code>
          （至少 12 个字符）后重启服务，即可使用管理功能。
        </p>
        <p class="state-note">
          未启用时，管理接口会拒绝登录和写入操作。
        </p>
        <RouterLink class="button button-primary" to="/">返回阅读</RouterLink>
      </section>

      <AdminLogin
        v-else-if="phase === 'login'"
        :busy="loginBusy"
        :error="loginError"
        @submit="submitLogin"
      />

      <div
        v-if="workspaceMounted"
        v-show="phase === 'ready'"
        class="admin-workspace"
      >
        <AdminBrowser
          v-show="!activePath && !trashOpen"
          ref="browserRef"
          @edit="openEditor"
          @view="viewDocument"
          @renamed="onRenamed"
          @notice="notify"
          @auth-expired="handleAuthExpired"
          @open-trash="openTrash"
        />
        <AdminTrash
          v-if="trashOpen"
          @back="closeTrash"
          @notice="notify"
          @auth-expired="handleAuthExpired"
        />
        <AdminEditor
          v-if="activePath"
          :key="activePath"
          :path="activePath"
          @close="closeEditor"
          @dirty="editorDirty = $event"
          @saved="onSaved"
          @notice="notify"
          @auth-expired="handleAuthExpired"
        />
      </div>
    </main>

    <ToastNotice :message="message" />
    <AdminThemeDialog
      :open="themeOpen"
      :theme="site.theme"
      @close="themeOpen = false"
      @saved="onThemeSaved"
      @auth-expired="handleAuthExpired"
    />
    <ConfirmDialog
      :open="confirmOpen"
      :title="confirmTitle"
      :description="confirmDescription"
      :confirm-text="confirmText"
      :danger="confirmDanger"
      @confirm="confirmPending"
      @cancel="cancelConfirm"
    />
  </div>
</template>
