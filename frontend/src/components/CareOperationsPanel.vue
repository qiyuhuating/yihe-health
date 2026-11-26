<template>
  <section v-if="isSupervisor && !forbidden" class="care-operations" aria-labelledby="care-operations-title">
    <header class="care-operations-head">
      <div><h3 id="care-operations-title">值守与服务诊断</h3><p>以下为服务端任务统计，不代表人员到岗或通知送达。</p></div>
      <el-button plain :loading="loading" @click="refresh">检查服务</el-button>
    </header>
    <p v-if="error" class="care-error" role="alert">{{ error }}</p>
    <p v-if="diagnostics?.primaryEngine === 'mysql'">核心业务主库：MySQL。</p>
    <p v-if="diagnostics?.analytics" :role="diagnostics.analytics.ready ? 'status' : 'alert'" :class="{'care-worker-critical': !diagnostics.analytics.ready}">
      {{ !diagnostics.analytics.configured ? 'PG 分析库未配置，分析事件保留在主库等待同步。' : diagnostics.analytics.ready ? 'PG 分析库可用。' : 'PG 分析库暂不可用，核心业务仍使用 MySQL，分析事件等待重试。' }}
      待同步：{{ diagnostics.analytics.pending == null ? '暂不可读' : diagnostics.analytics.pending }}。
      <span v-if="diagnostics.analytics.lastSyncedAt">最近同步：{{ new Date(diagnostics.analytics.lastSyncedAt).toLocaleString('zh-CN') }}。</span>
    </p>
    <p v-if="diagnostics && !diagnostics.workerHealthy" role="alert" class="care-worker-critical">告警服务进程心跳异常或尚未启动：不能保证通知和超时升级正在执行，必须立即检查常驻服务。</p>
    <p v-if="diagnostics && diagnostics.workerHealthy" role="status">告警服务进程最近一次心跳正常。此状态不代表消息已经送达。</p>
    <p v-if="diagnostics && diagnostics.notificationConfigured === false" role="alert" class="care-worker-critical">通知渠道尚未配置，任务通知无法发出。请联系主管配置通知网关，并按线下流程联系照护人员。</p>
    <p v-if="diagnostics && diagnostics.receiptConfigured === false" role="alert" class="care-worker-critical">送达回执校验尚未配置，当前无法确认渠道已送达。</p>
    <div v-if="diagnostics" class="care-operation-metrics">
      <span>待发送通知 <strong>{{ diagnostics.pendingNotifications }}</strong></span>
      <span>发送失败 <strong>{{ diagnostics.failedNotifications }}</strong></span>
      <span>发送中断 <strong>{{ diagnostics.staleSending }}</strong></span>
      <span>超时任务 <strong>{{ diagnostics.overdueIncidents }}</strong></span>
    </div>
    <div v-if="authorized" class="care-staff-settings">
      <div class="care-operations-head"><h4>岗位权限</h4><el-button plain @click="loadStaff">刷新人员</el-button></div>
      <p>主管可配置护理员或审计员。护理员可接单及记录处置；只有主管可复核关闭。</p>
      <div v-for="person in staff" :key="person.id" class="care-staff-row">
        <span>{{ person.name || '管理员' }}（编号 {{ person.id }}）</span>
        <el-select :model-value="person.duty" :disabled="assigning !== null" :aria-label="'岗位：'+person.id" @change="setDuty(person, $event)">
          <el-option label="未分配" value="unassigned" disabled />
          <el-option label="主管" value="supervisor" />
          <el-option label="护理员" value="caregiver" />
          <el-option label="审计员" value="auditor" />
        </el-select>
      </div>
      <el-button plain @click="showAudit = !showAudit; ifAudit()">查看敏感数据访问审计</el-button>
      <ol v-if="showAudit" class="care-access-list">
        <li v-for="(entry,index) in accesses" :key="index">
          {{ entry.actorId }} · {{ entry.resource }} · {{ new Date(entry.at).toLocaleString('zh-CN') }}
        </li>
      </ol>
    </div>
  </section>
  <section v-if="canAudit && !isSupervisor" class="care-operations" aria-labelledby="care-audit-title">
    <header class="care-operations-head">
      <div><h3 id="care-audit-title">访问审计</h3><p>审计岗位可查看记录，无权更改人员岗位或处置任务。</p></div>
      <el-button plain @click="ifAudit">刷新审计</el-button>
    </header>
    <p v-if="error" role="alert" class="care-error">{{ error }}</p>
    <ol class="care-access-list">
      <li v-for="(entry,index) in accesses" :key="index">
        {{ entry.actorId }} · {{ entry.resource }} · {{ new Date(entry.at).toLocaleString('zh-CN') }}
      </li>
    </ol>
  </section>
  <SecurityPanel />
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { isAxiosError } from 'axios'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import SecurityPanel from './SecurityPanel.vue'
import { startVisiblePolling } from '../composables/visiblePolling'
import { authState } from '../auth'
import { mayReadCareAudit, mayManageCareStaff } from '../domain/permissions'

type Duty = 'supervisor' | 'caregiver' | 'auditor'
interface Staff { id: number | string; name: string; duty: Duty | 'unassigned' }
interface AccessEntry { actorId: number | string; resource: string; at: string }
interface AnalyticsStatus {
  ready: boolean
  configured: boolean
  pending: number | null
  lastSyncedAt?: string
}
interface Diagnostics {
  primaryEngine?: string
  analytics?: AnalyticsStatus
  workerHealthy: boolean
  notificationConfigured?: boolean
  receiptConfigured?: boolean
  pendingNotifications: number
  failedNotifications: number
  staleSending: number
  overdueIncidents: number
}

const diagnostics = ref<Diagnostics | null>(null)
const staff = ref<Staff[]>([])
const accesses = ref<AccessEntry[]>([])
const authorized = ref(false)
const canAudit = computed(() => mayReadCareAudit(authState.account))
const isSupervisor = computed(() => mayManageCareStaff(authState.account))
const forbidden = ref(false)
const showAudit = ref(false)
const loading = ref(false)
const assigning = ref<number | string | null>(null)
const error = ref('')
const post = <T,>(url: string, body: Record<string, unknown> = {}) =>
  api.post<T>('api/v1/care/' + url, body)

let disposed = false
let staffRevision = 0
let auditRevision = 0
let stopPolling: (() => void) | undefined

async function refresh() {
  if (disposed || loading.value || !isSupervisor.value) return
  loading.value = true
  try {
    const { data } = await post<Diagnostics>('diagnostics')
    if (disposed) return
    diagnostics.value = data
    authorized.value = true
    forbidden.value = false
    error.value = ''
    await loadStaff()
  } catch (cause) {
    if (disposed) return
    authorized.value = false
    diagnostics.value = null
    if (isAxiosError(cause) && cause.response?.status === 403) {
      forbidden.value = true
      error.value = ''
    } else {
      error.value = apiErrorMessage(cause, '服务诊断暂不可用')
    }
  } finally {
    if (!disposed) loading.value = false
  }
}

async function loadStaff() {
  const revision = ++staffRevision
  try {
    const { data } = await post<{ staff: Staff[] }>('staff')
    if (disposed || revision !== staffRevision) return
    staff.value = Array.isArray(data.staff) ? data.staff : []
  } catch (cause) {
    if (disposed || revision !== staffRevision) return
    staff.value = []
    error.value = apiErrorMessage(cause, '人员权限列表未能加载')
  }
}

function setDuty(person: Staff, value: unknown) {
  if (value === 'supervisor' || value === 'caregiver' || value === 'auditor') {
    void assign(person, value)
  }
}

async function assign(person: Staff, duty: Duty) {
  if (disposed || assigning.value !== null || person.duty === duty) return
  assigning.value = person.id
  error.value = ''
  try {
    await post('assignDuty', { adminId: String(person.id), duty })
    if (!disposed) await loadStaff()
  } catch (cause) {
    if (!disposed) {
      await loadStaff()
      error.value = apiErrorMessage(cause, '岗位更改失败')
    }
  } finally {
    if (!disposed) assigning.value = null
  }
}

async function ifAudit() {
  const revision = ++auditRevision
  if (disposed || (!showAudit.value && isSupervisor.value) || !canAudit.value) return
  try {
    const { data } = await post<{ accesses: AccessEntry[] }>('accessAudit')
    if (!disposed && revision === auditRevision && canAudit.value) {
      accesses.value = Array.isArray(data.accesses) ? data.accesses : []
    }
  } catch (cause) {
    if (!disposed && revision === auditRevision && canAudit.value) {
      error.value = apiErrorMessage(cause, '审计记录暂不可用')
    }
  }
}

onMounted(() => {
  if (isSupervisor.value) void refresh()
  else if (canAudit.value) void ifAudit()
  stopPolling = startVisiblePolling(() => { void refresh() }, 15_000)
})
onBeforeUnmount(() => {
  disposed = true
  staffRevision++
  auditRevision++
  stopPolling?.()
})
</script>
<style scoped>
.care-worker-critical{padding:12px;background:#fff5ec;border-left:3px solid #b94d29;color:#8e3619;font-weight:600}
.care-operations{margin:16px 0;padding:18px;border:1px solid var(--care-border,#dce4da);border-radius:12px;background:#fff}
.care-operations-head{display:flex;justify-content:space-between;gap:12px;align-items:center;flex-wrap:wrap}
.care-operations-head h3,.care-operations-head h4{margin:0;color:var(--care-heading,var(--care-primary))}
.care-operations-head p,.care-staff-settings p{margin:4px 0;color:var(--care-text-muted,#65756e)}
.care-operation-metrics{display:flex;gap:16px;flex-wrap:wrap;margin:16px 0}
.care-operation-metrics span{padding:12px;border-radius:8px;background:#f6f8f5}
.care-staff-row{display:flex;justify-content:space-between;align-items:center;padding:10px 0;border-top:1px solid #e4e9e2;gap:12px}
.care-staff-row .el-select{max-width:150px}
.care-access-list{max-height:200px;overflow:auto;font-size:13px;color:#47584f}
</style>
