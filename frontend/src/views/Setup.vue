<template>
  <main class="setup-page">
    <section class="setup-story" aria-label="颐和">
      <img src="/static/brand/yihe-logo-512.webp" width="280" height="280" alt="颐和山水标志" decoding="async">
      <p>岁月有伴，日常安宁</p>
      <h1>从此刻，<br>安放每一份牵挂。</h1>
    </section>
    <section class="setup-panel">
      <span class="setup-kicker">初次相见 · 颐和</span>
      <h2>开启照护日常</h2>
      <database-notice><p class="setup-intro">创建管理员账号。业务记录保存在独立数据库中，关闭软件不会清除记录。</p></database-notice>
      <el-form ref="form" :model="account" :disabled="busy" label-position="top" :rules="rules" @submit.prevent="submit">
        <el-form-item label="怎么称呼您" prop="name"><el-input v-model="account.name" maxlength="10" autocomplete="name" /></el-form-item>
        <el-form-item label="登录手机号" prop="phone"><el-input v-model="account.phone" maxlength="11" inputmode="tel" autocomplete="username" /></el-form-item>
        <el-form-item label="设置密码" prop="password"><el-input v-model="account.password" type="password" show-password autocomplete="new-password" /></el-form-item>
        <p class="setup-hint">12–20 位，包含英文字母与数字。请妥善保管，软件不设通用密码。</p>
        <p v-if="error" role="alert" class="setup-error">{{ error }}</p>
        <el-button type="primary" native-type="submit" :loading="busy">创建账号，开始使用 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </el-form>
      <p class="setup-footnote">您的健康与照护记录，始终由您掌握。</p>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ArrowRight } from '@element-plus/icons-vue'
import { reactive, ref } from 'vue'
import type { FormInstance } from 'element-plus'
import { useRouter } from 'vue-router'
import { api } from '../api/client'
import { loadSession } from '../auth'
import { createAdministrator } from '../desktop'
import { setupFailureMessage } from '../api/setupFailure'

const router = useRouter()
const form = ref<FormInstance | null>(null)
const account = reactive({ name: '', phone: '', password: '' })
const busy = ref(false)
const error = ref('')
const rules = {
  name: [{ required: true, message: '请输入称呼', trigger: 'blur' }],
  phone: [{ pattern: /^1\d{10}$/, required: true, message: '请输入有效的 11 位手机号', trigger: 'blur' }],
  password: [{ pattern: /^(?=.*[A-Za-z])(?=.*\d).{12,20}$/, required: true, message: '请设置 12–20 位含字母和数字的密码', trigger: 'blur' }]
}
async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  let accountCreated = false
  try {
    if (!await form.value?.validate().catch(() => false)) return
    const submitted = { ...account }
    await createAdministrator(submitted)
    accountCreated = true
    account.password = ''
    await api.post('/api/v1/auth/login', { phone: submitted.phone, password: submitted.password })
    const loggedIn = await loadSession(true)
    if (!loggedIn) throw new Error('Session missing')
    await router.replace('/operations/overview')
  } catch (cause) {
    error.value = setupFailureMessage(accountCreated, cause)
  } finally { busy.value = false }
}
</script>

<style scoped>
.setup-page { min-height:100vh; display:grid; grid-template-columns:1fr 1fr; background:#f8f8f2; color:var(--care-brand-dark); }
.setup-story { display:flex; flex-direction:column; justify-content:center; align-items:center; padding:60px 40px; background:#edece1; text-align:center; }
.setup-story img { max-width:70%; height:auto; mix-blend-mode:multiply; }
.setup-story p { margin-top:36px; font-size:16px; letter-spacing:.15em; }
.setup-story h1 { margin:14px 0 0; font-size:38px; line-height:1.8; font-weight:500; }
.setup-panel { width:100%; max-width:550px; align-self:center; justify-self:center; padding:56px; }
.setup-kicker { font-size:14px; letter-spacing:.14em; color:var(--care-text-muted); }
.setup-panel h2 { margin:18px 0; font-size:32px; font-weight:500; }
.setup-intro { color:var(--care-text-muted); line-height:1.9; margin-bottom:30px; }
.setup-hint,.setup-footnote { color:var(--care-text-muted); font-size:14px; line-height:1.8; }
.setup-footnote { margin:28px 0 0; }
.setup-error { color:var(--care-danger); line-height:1.7; }
.setup-panel :deep(.el-input__wrapper) { min-height:50px; padding:0 14px; }
.setup-panel :deep(.el-form-item__label) { font-size:16px; }
.setup-panel :deep(.el-button) { width:100%; min-height:54px; margin-top:18px; font-size:17px; }
.setup-panel :deep(.el-button i) { margin-left:16px; }
@media(max-width:760px) { .setup-page { grid-template-columns:1fr; }.setup-story { padding:26px 20px; }.setup-story img { width:90px; }.setup-story p { margin:12px 0 0; }.setup-story h1 { display:none; }.setup-panel { padding:32px 24px; } }
</style>
