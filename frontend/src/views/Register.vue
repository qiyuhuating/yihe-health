<template>
  <div class="register-page">
    <section class="register-story">
      <img :src="heroImage" alt="晨光中的湖面与绿树步道" width="1536" height="1024" :srcset="heroImageSrcset" sizes="(max-width: 760px) 100vw, 54vw" fetchpriority="high" decoding="async">
      <div class="register-story-shade"></div>
      <div class="register-story-copy"><span>让关爱，成为日常</span><h1>把牵挂<br>变成安心的陪伴</h1><p>使用管理员提供的邀请码，连接家人的照护信息。</p></div>
    </section>
    <main class="register-panel">
      <div class="register-card">
        <div class="register-title">
          <div class="register-brand-row"><div class="register-brand"><img class="register-mark" :src="brandLogo" :srcset="brandLogoSrcset" sizes="64px" width="64" height="64" alt="" aria-hidden="true" decoding="async"><span>颐和健康</span></div><el-button class="register-text-toggle" link :aria-label="largeText ? '切换为标准字号' : '切换为更大字号'" :aria-pressed="String(largeText)" @click="toggleTextSize">{{ largeText ? '标准字' : '大字' }}</el-button></div>
          <div class="register-sub">创建家属账号 · 需要管理员发放的邀请码</div>
        </div>

        <database-notice />
        <el-form
          label-position="top"
          :model="registerForm"
          :rules="rules"
          ref="registerFormRef"
          class="register-form"
          @submit.prevent="submitForm"
        >
          <el-form-item label="邀请码" prop="code" for="register-code">
            <el-input
              id="register-code" aria-label="邀请码" v-model="registerForm.code"
              placeholder="请输入管理员提供的邀请码"
              maxlength="20"
              clearable
              @blur="checkCode">
              <template #append><el-button :loading="checkingCode" :disabled="checkingCode" @click="checkCode">验证</el-button></template>
            </el-input>
            <div v-if="codeMessage" :class="codeOk ? 'tip-ok' : 'tip-error'" :role="codeOk ? 'status' : 'alert'" aria-live="polite">{{ codeMessage }}</div>
          </el-form-item>

          <el-form-item label="姓名" prop="familyName" for="register-familyName">
            <el-input id="register-familyName" aria-label="姓名" v-model="registerForm.familyName" maxlength="10" placeholder="请输入您的姓名"></el-input>
          </el-form-item>

          <el-form-item label="与老人关系" prop="familyRelation" for="register-familyRelation">
            <el-input id="register-familyRelation" aria-label="与老人关系" v-model="registerForm.familyRelation" maxlength="10" placeholder="如：儿子、女儿"></el-input>
          </el-form-item>

          <el-form-item label="性别" prop="familySex">
            <el-radio-group v-model="registerForm.familySex" aria-label="性别">
              <el-radio value="男">男</el-radio>
              <el-radio value="女">女</el-radio>
            </el-radio-group>
          </el-form-item>

          <el-form-item label="手机号" prop="familyPhone" for="register-familyPhone">
            <el-input id="register-familyPhone" aria-label="手机号" v-model="registerForm.familyPhone" inputmode="tel" autocomplete="tel" maxlength="11" placeholder="用于登录的手机号"></el-input>
          </el-form-item>

          <el-form-item label="密码" prop="familyPassword" for="register-familyPassword">
            <el-input :type="showPassword ? 'text' : 'password'" autocomplete="new-password" id="register-familyPassword" aria-label="密码" v-model="registerForm.familyPassword" placeholder="12-20位，须包含字母和数字"><template #suffix><button  type="button" class="password-visibility" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><el-icon aria-hidden="true"><View /></el-icon></button></template></el-input>
          </el-form-item>

          <el-form-item label="确认密码" prop="confirmPassword" for="register-confirmPassword">
            <el-input :type="showConfirmPassword ? 'text' : 'password'" autocomplete="new-password" id="register-confirmPassword" aria-label="确认密码" v-model="registerForm.confirmPassword" placeholder="请再次输入密码"><template #suffix><button  type="button" class="password-visibility" :aria-label="showConfirmPassword ? '隐藏确认密码' : '显示确认密码'" :aria-pressed="showConfirmPassword" @click="showConfirmPassword = !showConfirmPassword"><el-icon aria-hidden="true"><View /></el-icon></button></template></el-input>
          </el-form-item>

          <el-form-item label="联系地址" prop="familyAddress" for="register-familyAddress">
            <el-input id="register-familyAddress" aria-label="联系地址" v-model="registerForm.familyAddress" maxlength="20" placeholder="选填"></el-input>
          </el-form-item>

          <el-form-item>
            <el-button type="primary" class="green" :loading="submitting" native-type="submit" :disabled="submitting">创建账号</el-button>
            <el-button native-type="button" @click="goLogin">返回登录</el-button>
          </el-form-item>
        </el-form>
      </div>
      <footer class="register-footer">颐和健康 · 让关怀始终在线</footer>
    </main>
  </div>
</template>

<script setup lang="ts">
import { View } from '@element-plus/icons-vue'
import { reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormItemRule } from 'element-plus'
import { apiErrorMessage } from '../api/httpContract'
import { invitations } from '../api/invitations'
import { useTextSizePreference } from '../composables/useTextSizePreference'
import { validatePasswordComplexity, validatePhone } from '../utils/validation'

const router = useRouter()
const heroImage = '/static/img/care-lakeside-artisan.webp'
const heroImageSrcset = '/static/img/care-lakeside-artisan-640.webp 640w, /static/img/care-lakeside-artisan.webp 1536w'
const brandLogo = '/static/brand/yihe-logo-96.webp'
const brandLogoSrcset = '/static/brand/yihe-logo-96.webp 96w, /static/brand/yihe-logo-192.webp 192w'
const submitting = ref(false)
const checkingCode = ref(false)
const showPassword = ref(false)
const showConfirmPassword = ref(false)
const { largeText, toggleTextSize } = useTextSizePreference()
const codeMessage = ref('')
const codeOk = ref(false)
const verifiedCode = ref('')
const registerForm = reactive({
  code: '', familyName: '', familyRelation: '', familySex: '男',
  familyPhone: '', familyPassword: '', confirmPassword: '', familyAddress: ''
})
const registerFormRef = ref<FormInstance | null>(null)
const checkConfirm: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (!value) return callback(new Error('请再次输入密码'))
  if (value !== registerForm.familyPassword) return callback(new Error('两次输入的密码不一致'))
  callback()
}
const rules = {
  code: [{ required: true, message: '请输入邀请码', trigger: 'blur' }],
  familyName: [{ required: true, message: '请输入姓名', trigger: 'blur' }],
  familyRelation: [{ required: true, message: '请输入与老人的关系', trigger: 'blur' }],
  familySex: [{ required: true, message: '请选择性别', trigger: 'change' }],
  familyPhone: [
    { required: true, message: '请输入手机号', trigger: 'blur' },
    { validator: validatePhone, trigger: 'blur' }
  ],
  familyPassword: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 12, max: 20, message: '密码长度为12-20个字符', trigger: 'blur' },
    { validator: validatePasswordComplexity, trigger: 'blur' }
  ],
  confirmPassword: [{ required: true, message: '请再次输入密码', trigger: 'blur' }, { validator: checkConfirm, trigger: 'blur' }]
}
let codeRequest = 0
watch(() => registerForm.code, () => {
  codeRequest++
  checkingCode.value = false
  codeOk.value = false
  verifiedCode.value = ''
})
watch(() => registerForm.familyPassword, () => {
  if (registerForm.confirmPassword) void registerFormRef.value?.validateField('confirmPassword').catch(() => {})
})
async function checkCode() {
  const code = registerForm.code.trim()
  const revision = ++codeRequest
  codeMessage.value = ''
  codeOk.value = false
  if (!code) {
    checkingCode.value = false
    return
  }
  checkingCode.value = true
  try {
    const { data } = await invitations.checkCode({ code })
    if (revision !== codeRequest || code !== registerForm.code.trim()) return
    if (data.returnCode === '1111') {
      codeOk.value = true
      verifiedCode.value = code
      codeMessage.value = '邀请码有效，绑定老人：' + data.oldPersonName +
        (data.remainDays === -1 ? '（长期有效）' : '（剩余 ' + data.remainDays + ' 天）')
    } else {
      codeMessage.value = data.message || '邀请码无效'
    }
  } catch (error) {
    if (revision === codeRequest) codeMessage.value = apiErrorMessage(error, '邀请码暂时无法验证')
  } finally {
    if (revision === codeRequest) checkingCode.value = false
  }
}
async function submitForm() {
  if (submitting.value) return
  const valid = await registerFormRef.value?.validate().catch(() => false)
  if (!valid || submitting.value) return
  if (!codeOk.value || verifiedCode.value !== registerForm.code.trim()) {
    ElMessage({ type: 'warning', message: '请先验证邀请码是否可用' })
    return
  }
  submitting.value = true
  try {
    const { data } = await invitations.join({
      code: registerForm.code.trim(),
      familyName: registerForm.familyName.trim(),
      familyRelation: registerForm.familyRelation.trim(),
      familySex: registerForm.familySex,
      familyPhone: registerForm.familyPhone.trim(),
      familyPassword: registerForm.familyPassword,
      familyAddress: registerForm.familyAddress.trim()
    })
    if (data.returnCode !== '1111') {
      ElMessage({ type: 'error', message: data.message || '注册失败，请稍后重试' })
      void checkCode()
      return
    }
    const signedPhone = registerForm.familyPhone.trim()
    registerForm.familyPassword = ''
    registerForm.confirmPassword = ''
    ElMessage({ type: 'success', message: '注册成功！已绑定老人：' + data.oldPersonName + '，请使用手机号登录' })
    try { localStorage.setItem('login_phone', signedPhone) } catch { /* Registration remains successful. */ }
    await router.push('/sign-in')
  } catch (error) {
    ElMessage({ type: 'error', message: apiErrorMessage(error, '注册失败，请检查后端服务是否已启动') })
  } finally { submitting.value = false }
}
function goLogin() { void router.push('/sign-in') }
</script>

<style scoped>
.register-page { display: grid; grid-template-columns: minmax(0, .9fr) minmax(560px, 1.1fr); min-height: 100vh; background: #f8f8f2; }
.register-story { position: sticky; top: 0; height: 100vh; overflow: hidden; background: var(--care-brand-dark); }
.register-story img { width: 100%; height: 100%; object-fit: cover; object-position: 60% center; }
.register-story-shade { position: absolute; inset: 0; background: linear-gradient(90deg, rgba(13,36,27,.66), rgba(13,36,27,.54)); }
.register-story-copy { position: absolute; top: 22%; left: 9%; right: 8%; color: #fff; }
.register-story-copy span { font-size: 17px; letter-spacing: .1em; }
.register-story-copy h1 { margin: 24px 0 16px; font-size: 42px; line-height: 1.5; font-weight: 600; }
.register-story-copy p { max-width: 330px; font-size: 18px; line-height: 1.8; color: #fff; }
.register-panel { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 36px 36px 24px; }
.register-card { width: 100%; max-width: 600px; padding: 28px; border: 1px solid var(--care-border); border-radius: 16px; background: #fff; }
.register-brand-row { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.register-text-toggle { min-width: 64px; min-height: 48px; padding: 8px; border: 1px solid #c7d3c1; border-radius: 8px; color: var(--care-primary); font-size: 16px; font-weight: 600; }
.password-visibility { width: 48px; height: 48px; padding: 0; border: 0; border-radius: 6px; background: transparent; color: var(--care-primary); cursor: pointer; }
.register-form :deep(.el-input__suffix) { right: 0; }
.register-title { padding-bottom: 24px; }
.register-brand { display: flex; align-items: center; gap: 12px; color: var(--care-brand-dark); font-size: 23px; font-weight: 700; }
.register-mark { display: block; width: 64px; height: 64px; flex-shrink: 0; object-fit: contain; border-radius: 50%; }
.register-sub { margin-top: 14px; color: var(--care-text-muted); font-size: 17px; line-height: 1.8; }
.register-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 22px; padding-bottom: 4px; }
.register-form :deep(.el-form-item) { min-width: 0; margin-bottom: 24px; }
.register-form :deep(.el-form-item:first-child) , .register-form :deep(.el-form-item:nth-last-child(2)) , .register-form :deep(.el-form-item:last-child) { grid-column: 1 / -1; }
.register-form :deep(.el-form-item__label) { color: #43534b; font-size: 17px; line-height: 1.5; padding-bottom: 8px; }
.register-form :deep(.el-input__inner) { height: 50px; font-size: 18px; }
.register-form :deep(.el-button) { min-height: 48px; font-size: 17px; }
.register-form :deep(.el-radio__label) { font-size: 18px; }
.register-form :deep(.el-radio) { display: inline-flex; align-items: center; min-height: 48px; margin-right: 18px; }
.register-form :deep(.el-form-item:last-child .el-form-item__content) { display: flex; flex-wrap: wrap; gap: 10px; }
.register-form :deep(.el-form-item:last-child .el-button) { margin: 0; flex: 1; }
.tip-ok, .tip-error { margin-top: 6px; font-size: 16px; line-height: 1.6; }
.tip-ok { color: var(--care-primary); } .tip-error { color: var(--care-danger); }
.register-footer { margin-top: 24px; color: var(--care-text-muted); font-size: 16px; text-align: center; }
html.large-text .register-page .register-sub { font-size: 19px; }
html.large-text .register-page .register-form :deep(.el-form-item__label) ,
html.large-text .register-page .register-form :deep(.el-input__inner) { font-size: 20px; }
html.large-text .register-page .register-form :deep(.el-input__inner) { height: 56px; }
@media (max-width: 1000px) { .register-page { grid-template-columns: minmax(220px, .6fr) minmax(0, 1fr); } .register-panel { padding: 24px; } .register-story-copy h1 { font-size: 32px; } }
@media (max-width: 760px) { .register-page { display: block; } .register-story { position: relative; height: 160px; } .register-story-copy { top: 24px; left: 24px; } .register-story-copy span, .register-story-copy p { display: none; } .register-story-copy h1 { margin: 0; font-size: 27px; } .register-panel { padding: 22px 16px; } .register-card { padding: 24px 20px; } .register-form { grid-template-columns: 1fr; } }
</style>
