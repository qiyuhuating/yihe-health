<template>
  <section class="security-panel" aria-labelledby="security-title">
    <h3 id="security-title">双因素认证</h3>
    <p>启用后，登录还需要验证器动态码。恢复码只展示一次，请离线保管。</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-if="!relogin">
      <label for="security-password">当前密码</label>
      <input id="security-password" v-model="password" type="password" autocomplete="current-password" />
      <button type="button" :disabled="busy || !password" @click="send('setup')">生成验证器密钥</button>
      <template v-if="secret">
        <label for="security-secret">在验证器中手动添加此密钥</label>
        <input id="security-secret" :value="secret" readonly autocomplete="off" />
        <p>账号：颐和；6 位动态码，每 30 秒更新。请勿截图或转发密钥。</p>
      </template>
      <label for="security-otp">动态验证码</label>
      <input id="security-otp" v-model="otp" inputmode="numeric" autocomplete="one-time-code" maxlength="6" />
      <div class="security-actions">
        <button v-if="secret" type="button" :disabled="busy || otp.length !== 6" @click="send('confirm')">确认启用</button>
        <button type="button" :disabled="busy || !password || otp.length !== 6" @click="send('disable')">停用双因素认证</button>
      </div>
    </template>
    <template v-else>
      <p role="status">设置已更新，其他会话已撤销，请重新登录。</p>
      <template v-if="recoveryCodes.length">
        <label for="security-recovery">一次性恢复码（请现在保存）</label>
        <textarea id="security-recovery" :value="recoveryCodes.join('\n')" rows="8" readonly autocomplete="off" />
      </template>
      <a href="/#/sign-in">保存后重新登录</a>
    </template>
  </section>
</template>
<script setup lang="ts">
import { apiErrorMessage } from '../api/httpContract'
import { ref, onBeforeUnmount } from 'vue'
import { api, accepted, type Envelope } from '../api/client'
const password = ref(''), otp = ref(''), secret = ref(''), error = ref('')
const busy = ref(false), relogin = ref(false), recoveryCodes = ref<string[]>([])
interface FactorResult extends Envelope { secret?: string; recoveryCodes?: string[]; reloginRequired?: boolean }
async function send(action: 'setup' | 'confirm' | 'disable') {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const { data } = await api.post<FactorResult>('api/v1/security/factor', { action, password: password.value, otp: otp.value })
    accepted(data)
    if (action === 'setup') { secret.value = data.secret || ''; otp.value = '' }
    else {
      recoveryCodes.value = data.recoveryCodes || []; relogin.value = Boolean(data.reloginRequired)
      password.value = ''; otp.value = ''; secret.value = ''
    }
  } catch (cause) {
    error.value = apiErrorMessage(cause, '认证设置未保存')
  } finally { busy.value = false }
}
onBeforeUnmount(() => { password.value = ''; otp.value = ''; secret.value = ''; recoveryCodes.value = [] })
</script>
<style scoped>
.security-panel {
  display: grid;
  gap: 12px;
  max-width: 640px;
  margin-top: 24px;
  padding: 24px;
  border: 1px solid var(--care-border);
  border-radius: var(--care-card-radius);
  background: var(--care-surface);
  box-shadow: var(--care-shadow-sm);
  color: var(--care-text);
}
.security-panel h3 {
  margin: 0;
  color: var(--care-heading);
  font-size: 18px;
}
.security-panel p {
  margin: 0;
  color: var(--care-text-muted);
  line-height: 1.65;
}
.security-panel label {
  color: var(--care-text-secondary);
  font-size: 15px;
}
.security-panel input,
.security-panel textarea {
  box-sizing: border-box;
  width: 100%;
  min-height: var(--care-target-min);
  padding: 10px 14px;
  border: 1px solid var(--care-control-border);
  border-radius: var(--care-control-radius);
  background: var(--care-surface);
  color: var(--care-text);
  font: inherit;
}
.security-panel input:focus,
.security-panel textarea:focus {
  border-color: var(--care-primary);
  outline: 2px solid var(--care-primary);
  outline-offset: -1px;
}
.security-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.security-panel button {
  min-height: var(--care-target-min);
  padding: 10px 18px;
  border: 1px solid var(--care-control-border);
  border-radius: var(--care-control-radius);
  background: var(--care-success-bg);
  color: var(--care-primary);
  font-weight: 500;
  cursor: pointer;
}
.security-panel button:hover:not(:disabled) {
  border-color: var(--care-primary);
  background: var(--care-surface-hover);
}
.security-panel button:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.security-panel [role='alert'] {
  color: var(--care-danger);
}
.security-panel a {
  display: inline-flex;
  align-items: center;
  min-height: var(--care-target-min);
  color: var(--care-primary);
  text-underline-offset: 3px;
}
</style>
