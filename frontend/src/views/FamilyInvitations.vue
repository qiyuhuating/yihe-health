<template>
  <div class="invite">
    <header class="collection-heading"><div><h1>家属邀请码</h1><p>为家属生成专属邀请，让关心有一个清晰的入口。</p></div><el-button type="primary" aria-label="生成邀请码" :icon="Plus" @click="openCreateDialog">生成邀请码</el-button></header>
    <section class="invite-guide"><div><span class="guide-number">01</span><strong>选择长者</strong><p>指定家属需要关联的照护对象。</p></div><div><span class="guide-number">02</span><strong>生成邀请</strong><p>设置数量与有效期，按需分享。</p></div><div><span class="guide-number">03</span><strong>完成注册</strong><p>家属填写邀请码后关联长者。</p></div></section>
    <div v-if="listError" class="collection-feedback" role="alert"><span>{{ listError }}</span><el-button plain @click="searchInviteCodes">重新加载邀请码</el-button></div>
    <section class="collection-panel">
      <div class="collection-toolbar"><div class="collection-filters"><div><label for="invite-state">邀请状态</label><el-select id="invite-state" v-model="state" @change="searchInviteCodes()"><el-option label="全部状态" value="all"></el-option><el-option label="可使用" value="unused"></el-option><el-option label="已使用" value="used"></el-option><el-option label="已过期" value="expired"></el-option></el-select></div></div><div class="collection-tools"><span class="collection-caption">共 {{ total }} 条邀请记录</span><el-button plain :loading="listLoading" @click="searchInviteCodes">刷新</el-button></div></div>
    
    <el-dialog title="生成邀请码" v-model="createDialogVisible" :close-on-click-modal="!creating" :close-on-press-escape="!creating" :show-close="!creating" width="420px" @close="closeCreateDialog">
      <div v-if="oldPeopleError" class="collection-feedback" role="alert">{{ oldPeopleError }}<el-button plain @click="loadOldPeople">重试加载长者</el-button></div>
      <el-form label-width="100px" :model="createForm" :rules="rules" ref="createFormRef">
        <el-form-item label="绑定老人" for="invite-old" prop="oldId">
          <el-select id="invite-old" v-model="createForm.oldId" placeholder="请选择老人" style="width: 100%;">
            <el-option
              v-for="item in oldPeople"
              :key="item.oldId"
              :label="item.name + '（编号 ' + item.oldId + '）'"
              :value="item.oldId">
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item label="生成数量" for="invite-count" prop="count">
          <el-input-number id="invite-count" v-model="createForm.count" :min="1" :max="20"></el-input-number>
        </el-form-item>
        <el-form-item label="有效天数" for="invite-days" prop="expireDays">
          <el-input id="invite-days" v-model="createForm.expireDays" placeholder="留空表示长期有效"></el-input>
          <div class="tip-small">例如填 7 表示 7 天内有效</div>
        </el-form-item>
      </el-form>
      <template #footer><span  class="dialog-footer">
        <el-button :disabled="creating" @click="createDialogVisible = false">取 消</el-button>
        <el-button type="primary" :loading="creating" @click="createInviteCode()">生 成</el-button>
      </span></template>
    </el-dialog>

    <el-dialog title="邀请码生成成功" v-model="resultDialogVisible" width="420px">
      <div class="tip-small">请把下面的邀请码发给家属（每个邀请码只能使用一次）：</div>
      <div v-for="item in createdCodes" :key="item" class="code-line">
        <span class="code-text">{{ item }}</span>
        <el-button size="small" plain type="primary" @click="copyCode(item)">复制</el-button>
      </div>
      <template #footer><span  class="dialog-footer">
        <el-button type="primary" @click="resultDialogVisible = false">关 闭</el-button>
      </span></template>
    </el-dialog>

    <el-table v-if="!mobileLayout" class="invite-desktop-table" v-loading="listLoading" :data="inviteCodeList" style="width:100%">
      <template #empty><div  class="collection-empty"><strong>{{ listError ? '邀请记录暂时无法加载' : '当前状态下没有邀请记录' }}</strong><span>生成的邀请码会显示在这里，供您追踪使用情况。</span></div></template>
      <el-table-column prop="code" label="邀请码" min-width="160"><template #default="scope" ><strong class="invitation-table-code">{{ scope.row.code }}</strong></template></el-table-column>
      <el-table-column prop="oldPersonName" label="绑定长者" min-width="120"></el-table-column>
      <el-table-column label="生成信息" width="190"><template #default="scope" >{{ scope.row.createUser || '暂未提供' }}<small class="device-table-secondary">{{ scope.row.createTime || '暂无时间' }}</small></template></el-table-column>
      <el-table-column label="有效期" width="170"><template #default="scope" >{{ scope.row.expireTime === '' ? '长期有效' : scope.row.expireTime || '暂未提供' }}</template></el-table-column>
      <el-table-column label="状态" width="130"><template #default="scope" ><el-tag v-if="scope.row.status === 'unused'" type="success">可使用</el-tag><el-tag v-else-if="scope.row.status === 'used'" type="info">已使用</el-tag><el-tag v-else-if="scope.row.status === 'expired'" type="danger">已过期</el-tag><span v-else>状态未知</span><small v-if="scope.row.usedTime" class="device-table-secondary">{{ scope.row.usedTime }}</small></template></el-table-column>
      <el-table-column label="操作" width="190" fixed="right"><template #default="scope" ><div class="device-table-actions"><el-button plain type="primary" @click="copyCode(scope.row.code)">复制</el-button><el-button plain type="danger" :disabled="scope.row.status !== 'unused'" @click="deleteInviteCode(scope.row.id)">作废</el-button></div></template></el-table-column>
    </el-table>
    <div v-if="mobileLayout && inviteCodeList.length" class="invite-mobile-grid"><article v-for="item in inviteCodeList" :key="item.id" class="invite-record"><header><span class="collection-caption">家属注册邀请码</span><span class="connection-state" :class="item.status === 'unused' ? 'is-online' : ''">{{ item.status === 'unused' ? '可使用' : item.status === 'used' ? '已使用' : item.status === 'expired' ? '已过期' : '状态未知' }}</span></header><strong class="invite-code-value">{{ item.code }}</strong><dl><dt>绑定长者</dt><dd>{{ item.oldPersonName || '暂未提供' }}</dd><dt>生成人</dt><dd>{{ item.createUser || '暂未提供' }}</dd><dt>生成时间</dt><dd>{{ item.createTime || '暂无时间' }}</dd><dt>有效期</dt><dd>{{ item.expireTime === '' ? '长期有效' : item.expireTime || '暂未提供' }}</dd><dt v-if="item.usedTime">使用时间</dt><dd v-if="item.usedTime">{{ item.usedTime }}</dd></dl><div class="invite-record-actions"><el-button type="primary" plain @click="copyCode(item.code)">复制邀请码</el-button><el-button plain type="danger" :disabled="item.status !== 'unused'" @click="deleteInviteCode(item.id)">作废邀请</el-button></div></article></div>
    <div v-else-if="mobileLayout" class="invite-mobile-empty collection-empty"><strong>{{ listLoading ? '正在加载邀请…' : listError ? '邀请记录暂时无法加载' : '当前状态下没有邀请记录' }}</strong><p>生成的邀请码会显示在这里，方便您查看使用情况。</p></div>
    
    <div v-if="total > pageSize" class="collection-pagination">
      <el-pagination
        @size-change="handleSizeChange"
        @current-change="changePage"
        v-model:current-page="currentPage"
        :page-size="pageSize"
        layout="total, prev, pager, next" :pager-count="5"
        :total="total">
      </el-pagination>
    </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { Plus } from '@element-plus/icons-vue'
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { useCollectionLayout } from '../composables/useCollectionLayout'
import { copyText } from '../utils/clipboard'
import { apiErrorMessage } from '../api/httpContract'
import { invitations, type InvitationRecord, type InvitableResident } from '../api/invitations'

const mobileLayout = useCollectionLayout()
const state = ref('all')
const listLoading = ref(false)
const listError = ref('')
const creating = ref(false)
const oldPeopleError = ref('')
const createDialogVisible = ref(false)
const resultDialogVisible = ref(false)
const createdCodes = ref<string[]>([])
const oldPeople = ref<InvitableResident[]>([])
const inviteCodeList = ref<InvitationRecord[]>([])
const total = ref(0)
const pageSize = ref(10)
const currentPage = ref(1)
const createFormRef = ref<FormInstance | null>(null)
const createForm = reactive<{ oldId: number | string; count: number; expireDays: string }>({
  oldId: '', count: 1, expireDays: ''
})
const rules = { oldId: [{ required: true, message: '请选择要绑定的老人', trigger: 'change' }] }
let requestRevision = 0

async function searchInviteCodes(keepPage = false) {
  if (keepPage !== true) currentPage.value = 1
  const revision = ++requestRevision
  const selected = state.value
  listLoading.value = true
  listError.value = ''
  try {
    const { data } = await invitations.list({
      page: currentPage.value, pageSize: pageSize.value, state: selected
    })
    if (revision !== requestRevision || selected !== state.value) return
    if (!Array.isArray(data.inviteCodes)) throw new Error('missing invites')
    if (data.total !== undefined && (!Number.isSafeInteger(data.total) || data.total < 0)) throw new Error('invalid invitation count')
    if (data.total !== undefined) {
      const lastPage = Math.max(1, Math.ceil(data.total / pageSize.value))
      if (currentPage.value > lastPage) {
        currentPage.value = lastPage
        void searchInviteCodes(true)
        return
      }
    }
    inviteCodeList.value = data.inviteCodes
    total.value = data.total ?? data.inviteCodes.length
  } catch (error) {
    if (revision === requestRevision) listError.value =
      apiErrorMessage(error, '邀请记录暂时无法更新，请重试。')
  } finally {
    if (revision === requestRevision) listLoading.value = false
  }
}
async function loadOldPeople() {
  oldPeopleError.value = ''
  try {
    const { data } = await invitations.oldPersonList()
    oldPeople.value = Array.isArray(data.oldPeople) ? data.oldPeople : []
  } catch (error) {
    oldPeopleError.value = apiErrorMessage(error, '长者列表暂时无法加载')
  }
}
function openCreateDialog() {
  createDialogVisible.value = true
  void loadOldPeople()
}
function closeCreateDialog() {
  createFormRef.value?.resetFields()
  createForm.count = 1
  createForm.expireDays = ''
}
async function createInviteCode() {
  if (creating.value) return
  const valid = await createFormRef.value?.validate().catch(() => false)
  if (!valid) return
  const expireDays = String(createForm.expireDays || '').trim()
  if (expireDays && !/^\d+$/.test(expireDays)) {
    ElMessage({ type: 'warning', message: '有效天数只能填数字，留空表示长期有效' })
    return
  }
  creating.value = true
  try {
    const { data } = await invitations.create({
      oldId: createForm.oldId, count: createForm.count, expireDays
    })
    if (data.returnCode !== '1111') throw new Error(data.message || '生成邀请码失败')
    createdCodes.value = data.codes || []
    createDialogVisible.value = false
    resultDialogVisible.value = true
    await searchInviteCodes()
  } catch (error) {
    ElMessage({ type: 'error', message: apiErrorMessage(error, '生成邀请码失败，请稍后重试') })
  } finally { creating.value = false }
}
async function deleteInviteCode(id: number) {
  try {
    await ElMessageBox.confirm('作废后该邀请码将无法再用于注册，是否继续？', '提示', {
      confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning'
    })
  } catch { return }
  try {
    const { data } = await invitations.delete({ id })
    if (data.returnCode !== '1111') throw new Error(data.message || '作废失败')
    ElMessage({ type: 'success', message: '已作废' })
    await searchInviteCodes(true)
  } catch (error) {
    ElMessage({ type: 'error', message: apiErrorMessage(error, '请求未完成，请重试') })
  }
}
async function copyCode(code: string) {
  try {
    await copyText(code)
    ElMessage({ type: 'success', message: '已复制邀请码' })
  } catch {
    ElMessage({ type: 'warning', message: '复制失败，请手动记录。' })
  }
}
function handleSizeChange(value: number) {
  pageSize.value = value
  currentPage.value = 1
  void searchInviteCodes(true)
}
function changePage(value: number) {
  currentPage.value = value
  void searchInviteCodes(true)
}
onMounted(() => { void searchInviteCodes(); void loadOldPeople() })
onBeforeUnmount(() => { requestRevision++ })
</script>

<style scoped>
.invite .tip-small {
  color: #909399;
  font-size: 12px;
}
.invite .code-line {
  padding: 8px 0;
  font-size: 18px;
  letter-spacing: 2px;
  color: #52BAB5;
}
.invite .code-text {
  display: inline-block;
  min-width: 140px;
  font-weight: bold;
}
</style>
