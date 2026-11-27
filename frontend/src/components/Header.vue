<template>
  <header class="care-header">
    <router-link class="brand" :to="isFamily ? '/family-portal/overview' : '/operations/overview'" aria-label="颐和，返回首页">
      <img class="brand-mark" src="/static/brand/yihe-logo-96.webp" srcset="/static/brand/yihe-logo-96.webp 96w, /static/brand/yihe-logo-192.webp 192w" sizes="56px" width="56" height="56" alt="" aria-hidden="true" decoding="async">
      <span class="brand-copy">
        <strong>颐和</strong>
        <small>智慧养老管理平台</small>
      </span>
    </router-link>
    <div class="header-right">
      <el-button class="text-size-button" link :aria-label="largeText ? '切换为标准字号' : '切换为更大字号'" :aria-pressed="String(largeText)" @click="toggleTextSize">{{ largeText ? '标准字' : '大字' }}</el-button>
      <el-button class="settings-button" link aria-label="打开通用设置" @click="settings?.open()">设置</el-button>
      <span class="header-divider" aria-hidden="true"></span>
      <span class="user-avatar" aria-hidden="true">{{ userInitial }}</span>
      <span class="username">{{ login_username || '用户' }}</span>
      <el-button class="logout-button" aria-label="退出登录" link :loading="loggingOut" :disabled="loggingOut" @click="logout">退出</el-button>
    </div>
    <GeneralSettings ref="settings" />
  </header>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import GeneralSettings from './GeneralSettings.vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { authState, endSession } from '../auth'
import { apiErrorMessage } from '../api/httpContract'
import { useTextSizePreference } from '../composables/useTextSizePreference'

const router = useRouter()
const settings = ref<InstanceType<typeof GeneralSettings> | null>(null)
const loggingOut = ref(false)
const login_username = computed(() => authState.account?.displayName || '')
const { largeText, toggleTextSize } = useTextSizePreference()
const isFamily = computed(() => authState.account?.role === 'FAMILY')
const userInitial = computed(() => (login_username.value || '用').slice(0, 1))
async function logout() {
  if (loggingOut.value) return
  try {
    await ElMessageBox.confirm('退出后需要重新登录，确定退出吗？', '退出登录', {
      confirmButtonText: '退出', cancelButtonText: '继续使用', type: 'warning'
    })
  } catch { return }
  loggingOut.value = true
  try {
    await endSession()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '退出未能完成'))
  } finally {
    loggingOut.value = false
  }
  // endSession invalidates local state even when the response was lost.
  await router.push('/sign-in')
}
</script>

<style scoped>
.care-header {
  position: relative;
  z-index: 20;
  height: 72px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 32px;
  background: var(--care-surface);
  border-bottom: 1px solid var(--care-border);
  box-shadow: var(--care-shadow-sm);
}
.brand { display: inline-flex; align-items: center; gap: 12px; color: var(--care-text); text-decoration: none; min-width: 0; }
.brand:focus-visible { outline: var(--care-focus-width) solid var(--care-focus-color); outline-offset: 4px; border-radius: 6px; }
.brand-mark { display: block; width: 52px; height: 52px; flex-shrink: 0; object-fit: contain; border-radius: 50%; box-shadow: 0 0 0 2px var(--care-primary-soft); }
.brand-copy { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.brand-copy strong { font-size: 21px; font-weight: 600; letter-spacing: .1em; color: var(--care-brand-dark); }
.brand-copy small { color: var(--care-text-muted); font-size: 13px; letter-spacing: .04em; }
.header-right { display: flex; align-items: center; gap: 12px; color: var(--care-text-muted); flex-shrink: 0; }
.header-divider { width: 1px; height: 24px; background: var(--care-border); margin: 0 6px; }
.user-avatar { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 50%; background: var(--care-success-bg); border: 1px solid var(--care-success-border); color: var(--care-primary); font-weight: 600; }
.username { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; color: var(--care-text-secondary); }
.settings-button { min-width: var(--care-target-primary); min-height: var(--care-target-primary); }
.logout-button { min-width: var(--care-target-primary); min-height: var(--care-target-primary); color: var(--care-text-secondary); padding: 8px 6px; }
.logout-button:hover { color: var(--care-primary); }
.text-size-button { min-width: 64px; min-height: var(--care-target-primary); padding: 10px 12px; border: 1px solid var(--care-neutral-border); border-radius: var(--care-control-radius); color: var(--care-primary); font-weight: 500; }
.text-size-button:hover { background: var(--care-surface-hover); border-color: var(--care-primary); }
.text-size-button:focus-visible { outline: var(--care-focus-width) solid var(--care-focus-color); outline-offset: 2px; }
@media (max-width: 600px) {
  .care-header { min-height: 72px; height: auto; padding: 10px 16px; gap: 8px; }
  .header-divider, .brand-copy small, .username { display: none; }
  .brand-mark { width: 48px; height: 48px; }
  .header-right { gap: 7px; }
  .text-size-button { min-width: 64px; padding: 10px 8px; }
}
@media (max-width: 420px) {
  .brand-copy small { display: none; }
  .user-avatar { display: none; }
  .brand { gap: 8px; }
}
@media (max-width: 360px) {
  .brand-copy { display: none; }
  .brand-mark { width: 44px; height: 44px; }
  .text-size-button { min-width: 56px; }
  .care-header { padding-inline: 10px; }
  .header-right { gap: 4px; }
}
html.large-text .care-header .brand-copy strong { font-size: 24px; }
html.large-text .care-header .brand-copy small,
html.large-text .care-header .username,
html.large-text .care-header .text-size-button,
html.large-text .care-header .settings-button,
html.large-text .care-header .logout-button { font-size: 17px; }
</style>
