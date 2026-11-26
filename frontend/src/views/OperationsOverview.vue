<template>
  <div class="dashboard">
    <div class="page-heading">
      <div><h1>{{ workbenchTitle }}</h1><p class="page-subtitle">{{ workbenchDescription }}</p></div>
      <el-button v-if="!isRestricted" type="primary" :icon="Plus" @click="openNoticeEditor">发布公告</el-button>
    </div>
    <care-inbox v-if="!isUnassigned" />
    <section v-else aria-label="岗位状态" class="announcement-panel">
      <h2>账号尚未分配岗位</h2>
      <p>请联系照护主管分配岗位。当前账号不能查看照护任务或访问管理功能。</p>
      <SecurityPanel />
    </section>
    <div v-if="!isRestricted" class="dashboard-panels">
    <section class="overview-grid" aria-label="设备告警摘要">
      <article class="overview-card alert-card" v-loading="snapshotLoading && !snapshot" :aria-busy="snapshotLoading && !snapshot">
        <div class="metric-icon amber" aria-hidden="true"><el-icon aria-hidden="true"><WarningFilled /></el-icon></div>
        <div class="metric-copy"><span>设备异常类别计数</span><strong v-if="!snapshotError">{{ allCount == null ? '—' : allCount }}</strong><strong v-else class="metric-error" role="status">{{ snapshotError }}</strong><small v-if="categoryCounts">心率 {{ categoryCounts.rate }} · 血压 {{ categoryCounts.pressure }} · 睡眠 {{ categoryCounts.sleep }}</small><small>同一设备可能对应多类异常</small></div>
        <el-button v-if="snapshotError" link @click="loadSnapshot">重新加载设备统计</el-button><el-button v-else link @click="router.push('/operations/devices')">查看设备 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </article>
      <article class="overview-card fall-card" v-loading="snapshotLoading && !snapshot" :aria-busy="snapshotLoading && !snapshot">
        <div class="metric-icon rose" aria-hidden="true"><el-icon aria-hidden="true"><BellFilled /></el-icon></div>
        <div class="metric-copy"><span>紧急告警</span><strong v-if="!snapshotError">{{ num == null ? '—' : num }}</strong><strong v-else class="metric-error" role="status">{{ snapshotError }}</strong><small v-if="positionTotal != null">{{ positionTotal ? num + ' / ' + positionTotal + ' 位长者有紧急告警' : '暂无长者位置记录' }}</small><small>根据最近告警记录统计</small></div>
        <el-button v-if="snapshotError" link @click="loadSnapshot">重新加载告警统计</el-button><el-button v-else link @click="router.push('/operations/locations')">查看位置 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </article>
      <article class="overview-card care-card" v-loading="snapshotLoading && !snapshot" :aria-busy="snapshotLoading && !snapshot">
        <div class="metric-icon rose" aria-hidden="true"><el-icon aria-hidden="true"><Document /></el-icon></div>
        <div class="metric-copy"><span>照护待办</span><strong v-if="!snapshotError">{{ snapshot?.activeCareIncidents ?? '—' }}</strong><strong v-else class="metric-error" role="status">{{ snapshotError }}</strong><small v-if="snapshot">其中 {{ snapshot.overdueCareIncidents }} 项已超时 · {{ snapshot.failedNotifications }} 条通知失败</small><small>需要人工接单、处置或复核的闭环任务</small></div>
        <el-button v-if="snapshotError" link @click="loadSnapshot">重新加载待办</el-button><el-button v-else link @click="focusCareInbox">查看待办 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </article>
      <article class="overview-card device-card" v-loading="snapshotLoading && !snapshot" :aria-busy="snapshotLoading && !snapshot">
        <div class="metric-icon amber" aria-hidden="true"><el-icon aria-hidden="true"><Clock /></el-icon></div>
        <div class="metric-copy"><span>设备关注</span><strong v-if="!snapshotError">{{ snapshot?.offlineDevices ?? '—' }}</strong><strong v-else class="metric-error" role="status">{{ snapshotError }}</strong><small v-if="snapshot">共 {{ snapshot.activeDevices }} 台启用设备 · 低电量 {{ snapshot.lowBatteryDevices }} · 未绑定 {{ snapshot.unboundDevices }}</small><small>离线超过 2 分钟的启用设备</small></div>
        <el-button v-if="snapshotError" link @click="loadSnapshot">重新加载设备状态</el-button><el-button v-else link @click="router.push('/operations/devices')">查看设备 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button>
      </article>
      <article class="overview-note">
        <span class="note-icon" aria-hidden="true"><el-icon aria-hidden="true"><InfoFilled /></el-icon></span>
        <div><strong>数据说明</strong><p>健康与位置数据取决于手表最近一次上报。页面不会将未上报的数据标记为正常。</p></div>
      </article>
    </section>
    <section class="announcement-panel">
      <div class="panel-heading">
        <div><h2>平台公告 <span v-if="!isLoading && !loadError" class="notice-count" :aria-label="'共 ' + total + ' 条公告'">{{ total }}</span></h2><p>面向家属发布的服务与照护信息</p></div>
        <form class="search-box" role="search" @submit.prevent="loadNotices()"><label class="search-label" for="admin-notice-search">按标题查找公告</label><el-input id="admin-notice-search" v-model="searchText" placeholder="搜索公告标题" :disabled="isLoading" clearable @clear="loadNotices"><template #append><el-button :icon="Search" aria-label="搜索公告" :loading="isLoading" native-type="submit"></el-button></template></el-input></form>
      </div>
      <el-table class="desktop-announcements" v-loading="isLoading" :data="notices" @row-click="getDetail" row-class-name="clickable-row">
        <el-table-column type="index" label="序号" width="64" align="center"></el-table-column>
        <el-table-column prop="title" label="公告标题" min-width="220" show-overflow-tooltip>
          <template #default="scope"><span class="announcement-title">{{ scope.row.title || '未命名公告' }}</span></template>
        </el-table-column>
        <el-table-column prop="time" label="发布时间" width="175"></el-table-column>
        <el-table-column label="查看" width="110" align="right"><template #default="scope"><el-button link @click.stop="getDetail(scope.row)">查看详情 <el-icon aria-hidden="true"><ArrowRight /></el-icon></el-button></template></el-table-column>
        <template #empty><div class="table-empty" role="status"><img v-if="!loadError && !appliedSearch" src="/static/img/care-announcement-empty.webp" alt="" width="160" height="160" loading="lazy" decoding="async"><strong>{{ emptyTitle }}</strong><span>{{ emptyDescription }}</span><el-button v-if="loadError" plain @click="loadNotices">重新加载</el-button><el-button v-else-if="appliedSearch" plain @click="clearSearch">清除搜索条件</el-button></div></template>
      </el-table>
      <div class="mobile-announcements" v-loading="isLoading">
        <div v-if="!notices.length" class="table-empty" role="status"><img v-if="!loadError && !appliedSearch" src="/static/img/care-announcement-empty.webp" alt="" width="160" height="160" loading="lazy" decoding="async"><strong>{{ emptyTitle }}</strong><span>{{ emptyDescription }}</span><el-button v-if="loadError" plain @click="loadNotices">重新加载</el-button><el-button v-else-if="appliedSearch" plain @click="clearSearch">清除搜索条件</el-button></div>
        <button v-for="notice in notices" :key="notice.textId" type="button" class="mobile-notice" :aria-label="'查看详情：' + (notice.title || '未命名公告')" @click="getDetail(notice)"><span><strong>{{ notice.title || '未命名公告' }}</strong><time>{{ notice.time || '暂无发布时间' }}</time></span><el-icon aria-hidden="true"><ArrowRight /></el-icon></button>
      </div>
      <div class="pagination-wrap" v-if="total > pageSize"><el-pagination @size-change="handleSizeChange" @current-change="changePage" v-model:current-page="currentPage" :page-size="pageSize" layout="total, sizes, prev, pager, next" :total="total"></el-pagination></div>
    </section>
    </div>
  </div>
</template>
<style scoped>
.mobile-announcements { display: none; }
.mobile-notice { display: flex; width: 100%; min-height: 88px; justify-content: space-between; align-items: center; gap: 16px; padding: 20px 0; background: transparent; border: 0; border-top: 1px solid var(--care-border-subtle); font: inherit; text-align: left; color: var(--care-text); cursor: pointer; }
.mobile-notice span { min-width: 0; }
.mobile-notice strong { display: block; font-size: 16px; line-height: 1.7; overflow-wrap: anywhere; }
.mobile-notice time { display: block; margin-top: 6px; font-size: 14px; color: var(--care-text-muted); }
.mobile-notice:focus-visible { outline: var(--care-focus-width) solid var(--care-focus-color); outline-offset: 2px; }
.dashboard { color: var(--care-text); }
.page-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 20px; margin-bottom: 24px; }
.page-heading h1 { margin: 0; color: var(--care-heading); font-size: 28px; line-height: 1.4; font-weight: 500; letter-spacing: 0; }
.page-subtitle { margin: 9px 0 0; color: var(--care-text-muted); font-size: 14px; line-height: 1.6; }
.page-heading :deep(.el-button) { height: var(--care-target-primary); padding: 0 20px; border-radius: var(--care-control-radius); }
.overview-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-bottom: 28px; }
.overview-card { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; min-height: 142px; padding: 24px 26px; border: 1px solid var(--care-border); border-radius: var(--care-card-radius); background: var(--care-surface); box-shadow: var(--care-shadow-sm); transition: border-color var(--care-motion-fast) var(--care-ease), box-shadow var(--care-motion-fast) var(--care-ease), transform var(--care-motion-fast) var(--care-ease); }
.overview-card:hover { border-color: var(--care-neutral-border); box-shadow: var(--care-shadow-card); transform: translateY(-1px); }
.metric-icon { display: grid; place-items: center; width: 44px; height: 44px; flex: 0 0 44px; border-radius: 12px; font-size: 20px; }
.metric-icon.amber { background: var(--care-warning-bg); color: var(--care-warning-strong); }
.metric-icon.rose { background: var(--care-danger-bg); color: var(--care-danger); }
.metric-copy { display: flex; flex: 1; flex-direction: column; gap: 3px; min-width: 0; }
.metric-copy span { color: var(--care-text-muted); font-size: 14px; }
.metric-copy .metric-error { color: var(--care-danger); font-size: 16px; line-height: 1.7; }
.metric-copy strong { color: var(--care-heading); font-size: 30px; line-height: 1.15; font-weight: 600; font-variant-numeric: tabular-nums; }
.metric-copy small { color: var(--care-text-muted); font-size: 14px; line-height: 1.5; }
.overview-card :deep(.el-button.is-link) { min-height: var(--care-target-primary); flex: 0 0 auto; color: var(--care-primary); font-size: 14px; }
.overview-note { grid-column: 1 / -1; display: flex; gap: 12px; align-items: flex-start; padding: 16px 24px; border: 1px solid var(--care-border); border-radius: 12px; background: var(--care-surface-tint); }
.note-icon { color: var(--care-primary); font-size: 17px; margin-top: 2px; }
.overview-note strong { color: var(--care-heading); font-size: 14px; }
.overview-note p { margin: 4px 0 0; color: var(--care-text-muted); font-size: 14px; line-height: 1.65; }
.announcement-panel { padding: 24px; border: 1px solid var(--care-border); border-radius: var(--care-panel-radius); background: var(--care-surface); box-shadow: var(--care-shadow-sm); }
.panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 18px; margin-bottom: 16px; }
.panel-heading h2 { margin: 0; color: var(--care-heading); font-size: 18px; font-weight: 500; }
.panel-heading p { margin: 6px 0 0; color: var(--care-text-muted); font-size: 14px; }
.search-label { display: block; margin-bottom: 8px; font-size: 14px; color: var(--care-text-secondary); }
.notice-count { display: inline-block; min-width: 28px; margin-left: 6px; padding: 1px 8px; border-radius: 6px; background: var(--care-success-bg); color: var(--care-primary); font-size: 14px; text-align: center; vertical-align: middle; }
.search-box { width: min(100%, 280px); }
.announcement-title { color: var(--care-text); font-weight: 500; }
.table-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 28px 10px; color: var(--care-text-muted); }
.table-empty img { display: block; object-fit: contain; }
.table-empty strong { color: var(--care-heading); font-size: 17px; line-height: 1.6; }
.table-empty span { max-width: 420px; font-size: 15px; line-height: 1.8; white-space: normal; }
.pagination-wrap { display: flex; justify-content: flex-end; padding-top: 18px; }
@media (min-width: 1800px) {
  .dashboard-panels { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 3fr); gap: 24px; align-items: start; }
  .overview-grid { grid-template-columns: 1fr; margin-bottom: 0; }
  .announcement-panel { min-width: 0; }
}
@media (min-width: 901px) and (max-width: 1180px) {
  .overview-grid { grid-template-columns: 1fr; }
}
@media (max-width: 760px) {
  .overview-card { padding: 20px 18px; }
  .overview-note { padding: 18px; }
  .desktop-announcements { display: none; }
  .mobile-announcements { display: block; }
  .page-heading { align-items: flex-start; flex-direction: column; }
  .page-heading h1 { font-size: 25px; }
  .overview-grid { grid-template-columns: 1fr; }
  .overview-note { grid-column: auto; }
  .panel-heading { align-items: flex-start; flex-direction: column; }
  .search-box { width: 100%; }
  .announcement-panel { padding: 18px 14px; }
  .pagination-wrap { justify-content: center; overflow-x: auto; }
}
</style>
<script setup lang="ts">
import { ArrowRight, BellFilled, Clock, Document, InfoFilled, Plus, Search, WarningFilled } from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { createLatestRead } from '../composables/latestRead'
import { startVisiblePolling } from '../composables/visiblePolling'
import CareInbox from '@/components/CareInbox.vue'
import SecurityPanel from '@/components/SecurityPanel.vue'
import { authState } from '@/auth'
import { isRestrictedAdmin } from '@/domain/permissions'
import store from '@/store'
import type { NoticeRecord } from '../domain/notices'

interface NoticeListResponse {
  texts: NoticeRecord[]
  total?: number
}
const router = useRouter()
const searchText = ref('')
const appliedSearch = ref('')
const notices = shallowRef<NoticeRecord[]>([])
const total = ref(0)
const isLoading = ref(false)
const loadError = ref('')
const pageSize = ref(10)
const currentPage = ref(1)
interface OperationsSnapshot {
  health: { heartRate: number; bloodPressure: number; sleep: number }
  totalResidents: number
  activeLocationAlerts: number
  activeCareIncidents: number
  overdueCareIncidents: number
  pendingNotifications: number
  failedNotifications: number
  staleNotifications: number
  activeDevices: number
  offlineDevices: number
  lowBatteryDevices: number
  unboundDevices: number
  capturedAt: string
}

const snapshot = shallowRef<OperationsSnapshot | null>(null)
const snapshotLoading = ref(false)
const snapshotError = ref('')
const categoryCounts = computed(() => snapshot.value ? {
  rate: snapshot.value.health.heartRate,
  pressure: snapshot.value.health.bloodPressure,
  sleep: snapshot.value.health.sleep
} : null)
const allCount = computed(() => snapshot.value
  ? snapshot.value.health.heartRate + snapshot.value.health.bloodPressure + snapshot.value.health.sleep
  : null)
const num = computed(() => snapshot.value?.activeLocationAlerts ?? null)
const positionTotal = computed(() => snapshot.value?.totalResidents ?? null)
let stopSnapshotPolling: (() => void) | undefined

const isRestricted = computed(() => isRestrictedAdmin(authState.account))
const isUnassigned = computed(() => authState.account?.role === 'ADMIN' &&
  !['supervisor', 'caregiver', 'auditor'].includes(authState.account.duty ?? ''))
const workbenchTitle = computed(() => {
  if (isUnassigned.value) return '待授权账号'
  if (authState.account?.duty === 'auditor') return '照护审计台'
  return isRestricted.value ? '照护工作台' : '运营总览'
})
const workbenchDescription = computed(() => {
  if (isUnassigned.value) return '尚未获得照护岗位授权。'
  if (authState.account?.duty === 'auditor') return '查看照护任务与访问审计，不能修改岗位或处置任务。'
  if (authState.account?.duty === 'caregiver') return '接单、开始照护并记录处置过程，复核由主管完成。'
  return '查看设备异常与平台公告，及时跟进照护事项。'
})
const emptyTitle = computed(() => loadError.value ? '公告暂时无法加载' :
  appliedSearch.value ? '没有找到相关公告' : '暂时没有公告')
const emptyDescription = computed(() => loadError.value || (appliedSearch.value
  ? '试试其他标题关键词，或清除搜索条件查看全部公告。'
  : '发布后的服务信息和照护提醒会显示在这里。'))

const noticeReads = createLatestRead(
  (query: { page: number; pageSize: number; title: string }, signal: AbortSignal) =>
    api.post<NoticeListResponse>('api/v1/notices/list', query, { signal }),
  ({ data }) => {
    if (!Array.isArray(data.texts)) throw new Error('missing notices')
    notices.value = data.texts
    if (data.total !== undefined && (!Number.isSafeInteger(data.total) || data.total < 0)) throw new Error('invalid notice count')
    total.value = data.total ?? data.texts.length
    loadError.value = ''
  },
  error => {
    loadError.value = apiErrorMessage(error, '请检查服务连接后重试')
    notices.value = []
    total.value = 0
  },
  loading => { isLoading.value = loading }
)
function loadNotices(keepPage = false) {
  if (isRestricted.value) return
  if (keepPage !== true) currentPage.value = 1
  appliedSearch.value = searchText.value.trim()
  return noticeReads.run({
    page: currentPage.value, pageSize: pageSize.value, title: appliedSearch.value
  })
}
function clearSearch() { searchText.value = ''; void loadNotices() }
function handleSizeChange(value: number) {
  pageSize.value = value
  currentPage.value = 1
  void loadNotices(true)
}
function changePage(value: number) {
  currentPage.value = value
  void loadNotices(true)
}
function getDetail(row: NoticeRecord) {
  void router.push({ path: '/operations/notices/detail', query: { textId: row.textId, picId: row.picId } })
}
function openNoticeEditor() { void router.push('/operations/notices/new') }

const snapshotReads = createLatestRead(
  (_: Record<string, never>, signal: AbortSignal) =>
    api.post<{ snapshot: OperationsSnapshot }>('api/v1/operations/snapshot', {}, { signal }),
  ({ data }) => {
    const value = data.snapshot
    const counters = value && [
      value.health.heartRate, value.health.bloodPressure, value.health.sleep,
      value.totalResidents, value.activeLocationAlerts, value.activeCareIncidents,
      value.overdueCareIncidents, value.pendingNotifications, value.failedNotifications,
      value.staleNotifications, value.activeDevices, value.offlineDevices,
      value.lowBatteryDevices, value.unboundDevices
    ]
    if (!value || !counters?.every(item => Number.isSafeInteger(item) && item >= 0) ||
        value.activeLocationAlerts > value.totalResidents || value.offlineDevices > value.activeDevices) {
      throw new Error('invalid operations snapshot')
    }
    snapshot.value = value
    snapshotError.value = ''
  },
  error => {
    snapshot.value = null
    snapshotError.value = apiErrorMessage(error, '运营快照暂时无法加载')
  },
  loading => { snapshotLoading.value = loading }
)
function loadSnapshot() {
  if (isRestricted.value) return
  return snapshotReads.run({})
}
function refreshSnapshotWhenVisible() {
  if (!document.hidden && !snapshotLoading.value) void loadSnapshot()
}
function focusCareInbox() {
  document.getElementById('care-inbox-title')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

watch(() => store.state.alertRevision, refreshSnapshotWhenVisible)
onMounted(() => {
  if (!isRestricted.value) {
    void loadNotices()
    void loadSnapshot()
    stopSnapshotPolling = startVisiblePolling(refreshSnapshotWhenVisible, 30_000)
  }
})
onBeforeUnmount(() => {
  noticeReads.dispose()
  snapshotReads.dispose()
  stopSnapshotPolling?.()
})
</script>
