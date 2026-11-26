<template>
  <div class="auth-page">
    <section class="auth-story">
      <img class="story-image" :src="heroImage" alt="晨光洒向平静的湖面与绿树环绕的步道" width="1536" height="1024" srcset="/static/img/care-lakeside-artisan-640.webp 640w, /static/img/care-lakeside-artisan.webp 1536w" sizes="(max-width: 760px) 100vw, 54vw" fetchpriority="high" decoding="async">
      <div class="story-shade"></div>
      <div class="story-copy">
        <span class="story-kicker">让关爱，成为日常</span>
        <h1>岁月有伴<br>日常安宁</h1>
        <p>让健康数据更清晰，让家人的牵挂更及时。</p>
      </div>
      <span class="story-caption">温柔照护 · 彼此安心</span>
    </section>
    <main class="auth-panel">
      <div class="auth-card">
        <el-button class="login-text-toggle" link :aria-label="largeText ? '切换为标准字号' : '切换为更大字号'" :aria-pressed="String(largeText)" @click="toggleTextSize">{{ largeText ? '标准字' : '大字' }}</el-button>
        <div class="auth-brand"><img class="auth-mark" src="/static/brand/yihe-logo-96.webp" srcset="/static/brand/yihe-logo-96.webp 96w, /static/brand/yihe-logo-192.webp 192w" sizes="72px" width="72" height="72" alt="" aria-hidden="true" decoding="async"><span>颐和健康</span></div>
        <p class="auth-eyebrow">智慧养老管理平台</p>
        <h2>欢迎回来</h2>
        <database-notice><p class="auth-intro">登录后查看设备状态与家人近况</p></database-notice>
        <el-form label-position="top" ref="loginFormRef" :model="loginForm" :rules="rules" class="login-form" @submit.prevent="submitForm">
          <el-form-item label="手机号" prop="phone" for="login-phone">
            <el-input id="login-phone" aria-label="手机号" v-model="loginForm.phone" placeholder="请输入登录手机号" autocomplete="username" inputmode="tel" maxlength="11" clearable>
              <template #prefix><el-icon class="el-input__icon "><Iphone aria-hidden="true" /></el-icon></template>
            </el-input>
          </el-form-item>
          <el-form-item label="密码" prop="password" for="login-password">
            <el-input id="login-password" aria-label="密码" v-model="loginForm.password" :type="showPassword ? 'text' : 'password'" placeholder="请输入密码" autocomplete="current-password">
              <template #prefix><svg class="el-input__icon password-lock" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 4v3"/></svg></template><template #suffix><button  type="button" class="password-visibility" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><el-icon aria-hidden="true"><View /></el-icon></button></template>
            </el-input>
          </el-form-item>
          <el-form-item v-if="mfaRequired" label="动态验证码或恢复码" prop="otp">
            <div class="mfa-inputs">
              <el-input v-model="loginForm.otp" aria-label="动态验证码" autocomplete="one-time-code" inputmode="numeric" maxlength="6" placeholder="6 位动态验证码" />
              <el-input v-model="loginForm.recoveryCode" aria-label="一次性恢复码" autocomplete="off" placeholder="无法使用验证器时填写恢复码" />
            </div>
          </el-form-item>
          <p v-if="errorMessage" class="auth-error" role="alert">{{ errorMessage }}</p>
          <el-button type="primary" native-type="submit" class="submit-button" :loading="submitting" :disabled="submitting">登录</el-button>
        </el-form>
        <div class="auth-register"><span>首次使用？</span><el-button link @click="goRegister">使用邀请码注册</el-button></div>
        <p class="auth-footnote"><el-icon><Setting aria-hidden="true" /></el-icon> 账号信息仅用于本系统登录</p>
      </div>
      <footer class="auth-footer">颐和健康 · 让关怀始终在线</footer>
    </main>
  </div>
</template>

<script setup lang="ts">
import { Iphone, Setting, View } from '@element-plus/icons-vue'
import { reactive, ref, watch } from 'vue'
import type { FormInstance, FormItemRule } from 'element-plus'
import { useRouter } from 'vue-router'
import { apiErrorMessage } from '../api/httpContract'
import { api } from '../api/client'
import axios from 'axios'
import { loadSession } from '../auth'
import { useTextSizePreference } from '../composables/useTextSizePreference'
import { validateMfaCredentials, validatePasswordByteLimit, validatePhone } from '../utils/validation'

const router = useRouter()
let registeredPhone = ''
try {
  registeredPhone = localStorage.getItem('login_phone') || ''
  localStorage.removeItem('login_phone')
} catch { /* Login remains available when browser storage is blocked. */ }
const heroImage = '/static/img/care-lakeside-artisan.webp'
const submitting = ref(false)
const showPassword = ref(false)
const mfaRequired = ref(false)
const errorMessage = ref('')
const { largeText, toggleTextSize } = useTextSizePreference()
const loginForm = reactive({ phone: registeredPhone, password: '', otp: '', recoveryCode: '' })
const loginFormRef = ref<FormInstance | null>(null)
const validateMfa: NonNullable<FormItemRule['validator']> = (_rule, _value, callback) => {
  if (!mfaRequired.value) return callback()
  const issue = validateMfaCredentials(loginForm.otp, loginForm.recoveryCode)
  callback(issue ? new Error(issue) : undefined)
}
const rules: Record<string, FormItemRule[]> = {
  phone: [{ required: true, message: '请输入手机号', trigger: 'blur' }, { validator: validatePhone, trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { validator: validatePasswordByteLimit, trigger: 'blur' }],
  otp: [{ validator: validateMfa, trigger: 'blur' }]
}
function goRegister() { void router.push('/join-family') }
async function submitForm() {
  if (submitting.value) return
  submitting.value = true
  try {
    const valid = await loginFormRef.value?.validate().catch(() => false)
    if (!valid) return
    errorMessage.value = ''
    const payload = { phone: loginForm.phone.trim(), password: loginForm.password, otp: loginForm.otp.trim(), recoveryCode: loginForm.recoveryCode.trim() }
    const { data } = await api.post<{ authenticated?: boolean; message?: string }>('/api/v1/auth/login', payload)
    if (data?.authenticated === true) {
      const account = await loadSession(true)
      if (!account) throw new Error('登录状态未能建立，请刷新页面后重试')
      loginForm.password = ''
      loginForm.otp = ''
      loginForm.recoveryCode = ''
      await router.push(account.role === 'ADMIN' ? '/operations' : '/family-portal')
    } else {
      errorMessage.value = data?.message || '手机号或密码不正确，请重试'
    }
  } catch (error) {
    const raw: unknown = axios.isAxiosError(error) ? error.response?.data : undefined
    const detail = raw !== null && typeof raw === 'object' ? raw as { code?: unknown; message?: unknown } : null
    const code = typeof detail?.code === 'string' ? detail.code : ''
    const serverMessage = typeof detail?.message === 'string' ? detail.message : ''
    if (code === 'MFA_REQUIRED') {
      mfaRequired.value = true
      errorMessage.value = serverMessage || '需要进行二次验证'
    } else if (['INVALID_CREDENTIALS', 'INVALID_LOGIN_INPUT', 'ACCOUNT_UNLINKED'].includes(code)) {
      errorMessage.value = serverMessage || '登录失败，请重试'
    } else {
      errorMessage.value = apiErrorMessage(error, '暂时无法登录，请稍后重试')
    }
  } finally { submitting.value = false }
}
watch(() => [loginForm.phone, loginForm.password, loginForm.otp, loginForm.recoveryCode], () => {
  if (errorMessage.value) errorMessage.value = ''
})
</script>

<style scoped>
.mfa-inputs { display: flex; flex-direction: column; gap: 10px; width: 100%; }
.auth-page { display: grid; grid-template-columns: minmax(0, 1.08fr) minmax(430px, .92fr); height: 100vh; height: 100dvh; overflow: hidden; background: #f8f8f2; }
.auth-story { position: relative; min-height: 0; overflow: hidden; background: var(--care-brand-dark); }
.story-image { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; object-position: 60% center; }
.story-shade { position: absolute; inset: 0; background: linear-gradient(90deg, rgba(13,36,27,.66), rgba(13,36,27,.54)), linear-gradient(0deg, rgba(13,36,27,.34), transparent 60%); }
.story-copy { position: absolute; left: 9%; right: 8%; top: 23%; color: #fff; }
.story-kicker { display: inline-block; padding-left: 14px; border-left: 2px solid #dfd6b0; font-size: 17px; letter-spacing: .12em; }
.story-copy h1 { margin: 24px 0 20px; font-size: 44px; line-height: 1.5; letter-spacing: .03em; font-weight: 600; }
.story-copy p { margin: 0; font-size: 18px; line-height: 1.9; color: #fff; }
.story-caption { position: absolute; left: 9%; bottom: 40px; color: #fff; font-size: 16px; letter-spacing: .1em; }
.auth-panel { display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 0; overflow-y: auto; overscroll-behavior: contain; padding: clamp(16px, 4vh, 44px) 48px; }
.auth-card { position: relative; flex-shrink: 0; width: 100%; max-width: 460px; margin-block: auto; padding: 36px 36px 30px; border: 1px solid var(--care-border); border-radius: 20px; background: #fff; box-shadow: var(--care-shadow-card); }
.auth-brand { display: flex; align-items: center; gap: 11px; color: var(--care-brand-dark); font-size: 22px; font-weight: 700; letter-spacing: .04em; }
.auth-mark { display: block; width: 60px; height: 60px; flex-shrink: 0; object-fit: contain; border-radius: 50%; }
.auth-eyebrow { margin: clamp(14px, 3vh, 32px) 0 8px; color: var(--care-text-muted); font-size: 16px; letter-spacing: .06em; }
.auth-card h2 { margin: 0; color: var(--care-brand-dark); font-size: 34px; font-weight: 700; }
.auth-intro { margin: 8px 0 22px; color: var(--care-text-muted); font-size: 18px; line-height: 1.7; }
.login-form :deep(.el-form-item) { margin-bottom: 26px; }
.login-form :deep(.el-form-item__label) { color: #33453c; font-size: 17px; line-height: 1.5; padding-bottom: 8px; }
.login-form :deep(.el-input__inner) { height: 54px; font-size: 18px; }
.login-form :deep(.el-input__prefix) { color: var(--care-text-muted); }
.login-form :deep(.el-input__suffix) { right: 0; }
.login-form :deep(.el-input__suffix .el-input__icon) { width: 44px; min-height: 44px; color: var(--care-text-muted); }
.password-visibility { width: 48px; height: 48px; padding: 0; border: 0; border-radius: 6px; background: transparent; color: var(--care-primary); cursor: pointer; }
.password-lock { width: 16px; height: 16px; flex-shrink: 0; }
.submit-button { width: 100%; min-height: 54px; margin-top: 6px; border-radius: 9px; letter-spacing: .04em; font-size: 18px; font-weight: 600; }
.auth-error { margin: -5px 0 12px; color: var(--care-danger); font-size: 17px; line-height: 1.7; }
.auth-register { display: flex; align-items: center; justify-content: center; flex-wrap: wrap; gap: 4px; margin-top: 12px; color: var(--care-text-muted); font-size: 17px; }
.auth-register :deep(.el-button) { color: var(--care-primary); min-height: 48px; font-size: 17px; }
.auth-footnote { margin: 16px 0 0; color: var(--care-text-muted); text-align: center; font-size: 16px; }
.auth-footnote i { margin-right: 4px; }
.auth-footer { margin-top: 18px; color: var(--care-text-muted); font-size: 16px; text-align: center; }
.login-text-toggle { position: absolute; top: 28px; right: 28px; min-width: 64px; min-height: 48px; padding: 8px; border: 1px solid #c7d3c1; border-radius: 8px; color: var(--care-primary); font-size: 16px; font-weight: 600; }
html.large-text .auth-intro, html.large-text .login-form :deep(.el-form-item__label) , html.large-text .auth-register, html.large-text .auth-error { font-size: 20px; }
html.large-text .auth-card h2 { font-size: 38px; }
html.large-text .submit-button { min-height: 58px; font-size: 20px; }
@media (max-width: 1000px) { .auth-page { grid-template-columns: minmax(0, .8fr) minmax(400px, 1.2fr); } .story-copy h1 { font-size: 34px; } .auth-panel { padding: 36px 28px; } }
@media (max-width: 760px) { .auth-page { grid-template-columns: minmax(0, 1fr); grid-template-rows: minmax(0, 120px) minmax(0, 1fr); } .auth-story { height: auto; } .story-image { object-position: center 56%; } .story-copy { top: 12px; left: 24px; } .story-kicker, .story-copy p, .story-caption { display: none; } .story-copy h1 { margin: 0; font-size: 28px; line-height: 1.6; } .auth-panel { padding: 16px; justify-content: flex-start; } .auth-card { padding: 24px 20px; border-radius: 16px; } .login-text-toggle { top: 20px; right: 20px; } .auth-eyebrow { margin-top: 12px; } .auth-brand { font-size: 20px; } .auth-footer { margin-top: 12px; } html.large-text .auth-page .story-copy h1 { font-size: 30px; } }
@media (max-height: 860px) {
  .auth-footnote, .auth-footer { display: none; }
}
@media (max-height: 740px) {
  .auth-panel { padding-block: 16px; justify-content: flex-start; }
  .auth-mark { width: 48px; height: 48px; }
  .auth-eyebrow { margin: 12px 0 4px; }
  .auth-intro { margin: 6px 0 16px; }
  .auth-register { margin-top: 4px; }
}
@media (max-width: 760px) and (max-height: 820px) {
  .auth-page { grid-template-rows: minmax(0, 1fr); }
  .auth-story { display: none; }
}
@media (max-height: 640px) {
  .auth-intro { display: none; }
  .auth-eyebrow { display: none; }
  .auth-card h2 { margin-top: 12px; }
  .login-form { margin-top: 12px; }
  .login-form :deep(.el-form-item) { margin-bottom: 20px; }
}
</style>
