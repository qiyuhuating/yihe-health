<template>
  <div class="profile-page">
    <el-alert v-if="profileLoadError" :title="profileLoadError" type="error" :closable="false" show-icon></el-alert>
    <el-button v-if="profileLoadError" link @click="searchFamily">重新加载资料</el-button>
    <SecurityPanel />
    <section v-loading="isProfileLoading" class="profile-card" aria-label="个人资料">
      <div class="profile-summary">
        <div class="profile-avatar"><img v-if="imageUrl" :src="imageUrl" alt="当前账号头像" @error="imageUrl = ''"><span v-else>{{ (familyProfile.familyName || personName || '家').slice(0, 1) }}</span></div>
        <div class="profile-identity"><p>家属账号</p><h2>{{ familyProfile.familyName || personName || '个人资料' }}</h2><span>在这里管理您的联系资料与账号。</span></div>
      </div>
      <div class="profile-actions"><el-button type="primary" @click="openProfileEditor">修改资料</el-button><el-button plain @click="openPasswordEditor">修改密码</el-button><el-upload class="avatar-uploader" action="/api/v1/image/saveImage" :headers="uploadHeaders" :show-file-list="false" :with-credentials="true" :on-success="handleAvatarSuccess" :on-error="handleAvatarError" :before-upload="beforeAvatarUpload"><el-button plain>更换头像</el-button></el-upload></div>
      <dl class="profile-details"><div><dt>姓名</dt><dd>{{ familyProfile.familyName || '暂未填写' }}</dd></div><div><dt>性别</dt><dd>{{ familyProfile.familySex || '暂未填写' }}</dd></div><div><dt>手机号</dt><dd>{{ familyPhone || '暂未提供' }}</dd></div><div><dt>联系地址</dt><dd>{{ familyProfile.familyAddress || '暂未填写' }}</dd></div></dl>
    </section>
<el-dialog
				  title="编辑资料"
				  v-model="messageEditDialogVisible"
          :close-on-click-modal="!isProfileSaving" :close-on-press-escape="!isProfileSaving" :show-close="!isProfileSaving"
				  width="92%" class="profile-dialog">
				  <el-form label-width="80px" ref="profileEditFormRef" :rules="profileRules" :model="messageEditForm">
				    <el-form-item label="家属ID" prop="familyId" style="display: none; ">
				    <el-input v-model="messageEditForm.familyId"></el-input>
				    </el-form-item>
				    <el-form-item label="姓名" for="profile-name" prop="familyName" >
				    <el-input id="profile-name" maxlength="10" autocomplete="name" aria-label="姓名" v-model="messageEditForm.familyName"></el-input>
				    </el-form-item>
				    <el-form-item label="性别" for="profile-sex" prop="familySex">
				    <el-select popper-class="profile-sex-options" id="profile-sex" aria-label="性别" v-model="messageEditForm.familySex"><el-option label="男" value="男"></el-option><el-option label="女" value="女"></el-option></el-select>
				    </el-form-item>
						<el-form-item label="地址" for="profile-address" prop="familyAddress">
						<el-input id="profile-address" maxlength="20" autocomplete="street-address" aria-label="联系地址" v-model="messageEditForm.familyAddress"></el-input>
						</el-form-item>
						<el-form-item label="电话" for="profile-phone" prop="familyPhone">
						<el-input id="profile-phone" aria-label="手机号" v-model="messageEditForm.familyPhone" disabled="disabled"></el-input>
						</el-form-item>
				  </el-form>
				  <template #footer><span  class="dialog-footer">
				  <el-button :disabled="isProfileSaving" @click="messageEditDialogVisible = false">取 消</el-button>
				  <el-button type="primary" :loading="isProfileSaving" @click="editFamily">确 定</el-button>
				</span></template>
				</el-dialog>

				<el-dialog
				  title="修改密码"
				  v-model="passwordEditDialogVisible"
          :close-on-click-modal="!isPasswordSaving" :close-on-press-escape="!isPasswordSaving" :show-close="!isPasswordSaving" @closed="resetForm()"
				  width="92%" class="profile-dialog">
				  <el-form label-width="80px" status-icon :rules="rules2" ref="passwordEditFormRef" :model="passwordEditForm">
				    <el-form-item label="原密码" for="profile-old-password" prop="oldPassword" >
				    <el-input id="profile-old-password" type="password" aria-label="原密码" autocomplete="current-password" v-model="passwordEditForm.oldPassword"></el-input>
				    </el-form-item>
				    <el-form-item label="密码" for="profile-new-password" prop="familyPassword">
							<el-input id="profile-new-password" type="password" aria-label="新密码" autocomplete="new-password" v-model="passwordEditForm.familyPassword"></el-input>
						</el-form-item>
						<el-form-item label="确认密码" for="profile-confirm-password" prop="familyPassword2">
							<el-input id="profile-confirm-password" type="password" aria-label="确认密码" autocomplete="new-password" v-model="passwordEditForm.familyPassword2"></el-input>
						</el-form-item>
				  </el-form>
				  <template #footer><span  class="dialog-footer">
				  <el-button :disabled="isPasswordSaving" @click="resetForm()">取 消</el-button>
				  <el-button type="primary" :loading="isPasswordSaving" @click="editPassword()">确 定</el-button>
				</span></template>
				</el-dialog>

			  </div>
</template>
<style scoped>
.profile-card { padding: 32px; border: 1px solid var(--care-border); border-radius: 16px; background: #fff; }
.profile-summary { display: flex; align-items: center; gap: 22px; }
.profile-avatar { display: grid; place-items: center; width: 88px; height: 88px; flex: 0 0 88px; overflow: hidden; border-radius: 50%; background: #e6eddd; color: var(--care-primary); font-size: 34px; font-weight: 600; }
.profile-avatar img { width: 100%; height: 100%; object-fit: cover; }
.profile-identity { min-width: 0; overflow-wrap: anywhere; }
.profile-identity p { margin: 0 0 6px; color: #59665f; font-size: 16px; }
.profile-identity h2 { margin: 0 0 6px; color: var(--care-brand-dark); font-size: 28px; }
.profile-identity span { color: var(--care-text-muted); font-size: 17px; line-height: 1.7; }
.profile-actions { display: flex; flex-wrap: wrap; gap: 12px; margin: 28px 0; }
.profile-actions :deep(.el-button) { margin: 0; }
.profile-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 32px; margin: 0; }
.profile-details div { padding: 20px 0; border-top: 1px solid #e8ece4; }
.profile-details dt { color: #59665f; font-size: 16px; }
.profile-details dd { margin: 6px 0 0; color: var(--care-text); font-size: 19px; line-height: 1.7; overflow-wrap: anywhere; }
@media (max-width: 600px) { .profile-card { padding: 24px; } .profile-summary { align-items: flex-start; gap: 16px; } .profile-avatar { width: 64px; height: 64px; flex-basis: 64px; font-size: 28px; } .profile-identity h2 { font-size: 24px; } .profile-actions { gap: 10px; } .profile-actions :deep(.el-button) { width: 100%; } .profile-actions > * { flex: 1 0 100%; } .profile-actions :deep(.el-upload) { display: block; } .profile-details { grid-template-columns: 1fr; } }
html.large-text .care-family .profile-identity span, html.large-text .care-family .profile-details dd { font-size: 21px; }
</style>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, type FormInstance, type FormItemRule } from 'element-plus'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { authState } from '../auth'
import SecurityPanel from '../components/SecurityPanel.vue'

interface FamilyProfileData {
  familyId?: string | number
  familyName?: string
  familySex?: string
  familyAddress?: string
  familyTouPic?: string
}
const uploadHeaders = computed(() => ({ [authState.csrfHeader]: authState.csrfToken }))
const imageUrl = ref('')
const isProfileLoading = ref(false)
const isProfileSaving = ref(false)
const profileLoadError = ref('')
const isPasswordSaving = ref(false)
const messageEditDialogVisible = ref(false)
const passwordEditDialogVisible = ref(false)
const messageEditForm = reactive({
  familyId: '', familyPhone: '', familyName: '', familySex: '', familyAddress: ''
})
const passwordEditForm = reactive({
  familyPassword: '', familyPassword2: '', oldPassword: ''
})
const profileEditFormRef = ref<FormInstance | null>(null)
const passwordEditFormRef = ref<FormInstance | null>(null)
const personName = computed(() => authState.account?.displayName || '')
const familyPhone = computed(() => authState.account?.phone || '')
const familyTouPic = ref('')
const familyProfile = ref<FamilyProfileData>({})
const profileRules = {
  familyName: [
    { required: true, whitespace: true, message: '请输入姓名', trigger: 'blur' },
    { max: 10, message: '姓名最多10个字符', trigger: 'blur' }
  ],
  familySex: [{ required: true, message: '请选择性别', trigger: 'change' }],
  familyAddress: [{ max: 20, message: '地址最多20个字符', trigger: 'blur' }]
}
const validatePass: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (!value) return callback(new Error('请输入密码'))
  if (passwordEditForm.familyPassword2) void passwordEditFormRef.value?.validateField('familyPassword2').catch(() => {})
  callback()
}
const validatePass2: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (!value) return callback(new Error('请再次输入密码'))
  if (value !== passwordEditForm.familyPassword) return callback(new Error('两次输入密码不一致!'))
  callback()
}
const rules2 = {
  oldPassword: [{ required: true, message: '请输入原密码', trigger: 'blur' }],
  familyPassword: [
    { validator: validatePass, trigger: 'blur' },
    { pattern: /^(?=.*[A-Za-z])(?=.*[0-9]).{12,20}$/, message: '请设置 12–20 位含字母和数字的密码', trigger: 'blur' }
  ],
  familyPassword2: [
    { validator: validatePass2, trigger: 'blur' },
    { pattern: /^(?=.*[A-Za-z])(?=.*[0-9]).{12,20}$/, message: '请设置 12–20 位含字母和数字的密码', trigger: 'blur' }
  ]
}
let profileReadRevision = 0
async function searchFamily() {
  const revision = ++profileReadRevision
  const requestedPhone = familyPhone.value
  isProfileLoading.value = true
  profileLoadError.value = ''
  try {
    const { data } = await api.post<{ familyDto2?: FamilyProfileData }>('api/v1/family/selectTwo', { familyPhone: requestedPhone })
    if (revision !== profileReadRevision || requestedPhone !== familyPhone.value) return
    familyProfile.value = data?.familyDto2 || {}
    const profile = familyProfile.value
    if (profile.familyName && authState.account) authState.account.displayName = profile.familyName
    familyTouPic.value = profile.familyTouPic || ''
    imageUrl.value = familyTouPic.value && familyTouPic.value !== 'undefined' && familyTouPic.value !== 'null'
      ? '/api/v1/image/download?name=' + encodeURIComponent(familyTouPic.value) : ''
  } catch (error) {
    if (revision === profileReadRevision && requestedPhone === familyPhone.value) {
      profileLoadError.value = apiErrorMessage(error, '个人资料暂时无法加载，请重试。')
    }
  } finally {
    if (revision === profileReadRevision) isProfileLoading.value = false
  }
}
async function editFamily() {
  if (isProfileSaving.value) return
  const valid = await profileEditFormRef.value?.validate().catch(() => false)
  if (!valid) return
  isProfileSaving.value = true
  try {
    const { data } = await api.post<Envelope>('api/v1/family/updateTwo', { ...messageEditForm })
    accepted(data)
    messageEditDialogVisible.value = false
    ElMessage.success('个人资料已保存')
    await searchFamily()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '资料暂时无法保存，请稍后重试'))
  } finally { isProfileSaving.value = false }
}
async function handleAvatarSuccess(res: { returnCode?: string; filename?: string } | null) {
  if (!res || res.returnCode !== '1111' || !res.filename) {
    ElMessage.error(apiErrorMessage({ cause: res }, '头像上传未成功'))
    return
  }
  try {
    const { data } = await api.post<Envelope>('api/v1/family/updateTouPic', {
      familyTouPic: res.filename, familyPhone: familyPhone.value
    })
    accepted(data)
    ElMessage.success('头像已更新')
    await searchFamily()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '头像暂时无法更新，请稍后重试'))
  }
}
function handleAvatarError(error: unknown) {
  ElMessage.error(apiErrorMessage(error, '头像暂时无法上传，请稍后重试'))
}
async function editPassword() {
  if (isPasswordSaving.value) return
  const valid = await passwordEditFormRef.value?.validate().catch(() => false)
  if (!valid || isPasswordSaving.value) return
  isPasswordSaving.value = true
  try {
    const { data } = await api.post<Envelope>('api/v1/family/updatePassword', {
      familyPhone: familyPhone.value,
      oldPassword: passwordEditForm.oldPassword,
      familyPassword: passwordEditForm.familyPassword
    })
    accepted(data)
    resetForm()
    ElMessage.success('修改密码成功')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '密码暂时无法修改，请稍后重试'))
  } finally { isPasswordSaving.value = false }
}
async function openProfileEditor() {
  const profile = familyProfile.value
  Object.assign(messageEditForm, {
    familyId: profile.familyId, familyName: profile.familyName,
    familySex: profile.familySex, familyAddress: profile.familyAddress,
    familyPhone: familyPhone.value
  })
  messageEditDialogVisible.value = true
  await nextTick()
  profileEditFormRef.value?.clearValidate()
}
function openPasswordEditor() {
  passwordEditDialogVisible.value = true
}
function resetForm() {
  passwordEditDialogVisible.value = false
  passwordEditForm.oldPassword = ''
  passwordEditForm.familyPassword = ''
  passwordEditForm.familyPassword2 = ''
  passwordEditFormRef.value?.clearValidate()
}
function beforeAvatarUpload(file: File) {
  const validType = ['image/jpeg', 'image/png'].includes(file.type)
  const validSize = file.size < 2 * 1024 * 1024
  if (!validType) ElMessage.error('请选择 JPG 或 PNG 格式的头像')
  if (!validSize) ElMessage.error('上传头像图片大小不能超过 2MB!')
  return validType && validSize
}
onMounted(() => { void searchFamily() })
onBeforeUnmount(() => { profileReadRevision++ })
</script>
