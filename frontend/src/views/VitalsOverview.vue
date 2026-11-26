<template>
  <div class="roles">

    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="loadVitals()">
        <div class="search-field">

          <label class="table-search-label" for="search-vitals">长者姓名</label>
          <el-input id="search-vitals"
            placeholder="按长者姓名查找"
            v-model="searchText"
            class="input-with-select">
            <template #append><el-button aria-label="查询长者姓名"

              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>

		<el-badge :value="rateCount" :hidden="rateCount === 0" class="item">
		  <el-button size="small" :type="Number(rateCount) > 0 ? 'danger' : 'default'" plain @click="showAlertByKind('rate')">心率异常</el-button>
		</el-badge>
		<el-badge :value="preCount" :hidden="preCount === 0" class="item">
			<el-button size="small" :type="Number(preCount) > 0 ? 'danger' : 'default'" plain @click="showAlertByKind('pre')">血压异常</el-button>
		</el-badge>
		<el-button size="small" plain type="primary" @click="togglePendingAlerts">{{ onlyPendingAlerts ? '显示本页全部记录' : '只看本页待处理异常' }}</el-button>
      </form>
    </div>

    <el-table v-if="!mobileLayout" class="adaptive-record-table" :data="displayedVitals"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无健康记录</strong><span>最新上报的心率与血压数据会显示在这里。</span></div></template>

      <el-table-column type="index" width="150" align="center"></el-table-column>
      <el-table-column prop="name" label="姓名" width="180" align="center"></el-table-column>

      <el-table-column prop="sex" label="性别" width="150" align="center"></el-table-column>
			<el-table-column prop="preState" label="血压状态" width="150" align="center">
				<template #default="scope" >
                  <span v-if="scope.row.preState==1" class="status-danger">异常</span>
                  <span v-else-if="scope.row.preHandled==1" class="status-handled">已处理</span>
                  <span v-else-if="scope.row.preState === 0 || scope.row.preState === '0'" class="status-normal">正常</span><span v-else class="status-unknown">暂无状态</span>
				</template>
			</el-table-column>
			<el-table-column prop="rateState" label="心率状态" width="150" align="center">
				<template #default="scope" >
					<span v-if="scope.row.rateState==1" class="status-danger">异常</span>
					<span v-else-if="scope.row.rateHandled==1" class="status-handled">已处理</span>
					<span v-else-if="scope.row.rateState === 0 || scope.row.rateState === '0'" class="status-normal">正常</span><span v-else class="status-unknown">暂无状态</span>
				</template>
			</el-table-column>
			<el-table-column prop="createTime" label="时间" width="170" align="center"></el-table-column>
      <el-table-column label="操作" align="center">
        <template #default="scope" >
          <el-button v-if="scope.row.rateState==1 || scope.row.preState==1"
                     size="small" plain type="danger" :icon="WarningFilled"
                     @click="showAlertRow(scope.row)">查看原因</el-button>
          <el-button v-if="scope.row.rateState==1 || scope.row.preState==1"
                     size="small" plain type="primary" :icon="Check"
                     @click="resolveRow(scope.row)">标记已处理</el-button>
          <el-button size="small" plain type="success" :icon="Check" aria-label="查看健康详情" @click="showRoleDialog(scope.row)">
            查看健康详情
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout" :records="displayedVitals" :fields='[{"key":"preState","label":"血压状态","state":"health","handled":"preHandled"},{"key":"rateState","label":"心率状态","state":"health","handled":"rateHandled"},{"key":"oldId","label":"长者编号"},{"key":"sex","label":"性别"}]' label="心率与血压记录" title-key="name" key-field="oldId" time-key="createTime">
      <template #actions="scope"  ><el-button v-if="scope.row.rateState==1 || scope.row.preState==1"
                     size="small" plain type="danger" :icon="WarningFilled"
                     @click="showAlertRow(scope.row)">查看原因</el-button>
<el-button v-if="scope.row.rateState==1 || scope.row.preState==1"
                     size="small" plain type="primary" :icon="Check"
                     @click="resolveRow(scope.row)">标记已处理</el-button>
<el-button size="small" plain type="success" :icon="Check" aria-label="查看健康详情" @click="showRoleDialog(scope.row)">
            查看健康详情
          </el-button></template>
    </record-cards>

    <div style="margin-top: 10px;text-align: center">
      <el-pagination layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleSizeChange"
        @current-change="changePage"
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total" />
    </div>

    <HealthAlertDialog :dialog="alertDialog" @resolve="resolveAlert" />

    <el-dialog title="健康记录详情" v-model="detailsVisible" @opened="resizeCharts" @closed="disposeCharts">
      <el-alert v-if="chartError" :title="chartError" type="warning" :closable="false" show-icon></el-alert>
      <div v-loading="chartLoading" class="record-chart-content"><div class="record-chart trend-chart" ref="primaryChartElement"></div><div class="record-chart trend-chart" ref="secondaryChartElement"></div></div>
      <template #footer><div  class="dialog-footer"><el-button type="primary" @click="detailsVisible = false">关闭详情</el-button></div></template>
    </el-dialog>
  </div>

</template>

<script setup lang="ts">
import { Check, Search, WarningFilled } from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { useRecordCharts } from '@/composables/useRecordCharts'
import { createLatestRead } from '@/composables/latestRead'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage, transportOutcome, UNCONFIRMED_MESSAGE } from '../api/httpContract'
import RecordCards from '@/components/RecordCards.vue'
import HealthAlertDialog from '@/components/records/HealthAlertDialog.vue'
import type { VitalAlertDetail, VitalAlertState } from '@/domain/vitals'

interface VitalRow {
  oldId: number | string
  name: string
  recordId?: number | string
  rateState?: number | string
  preState?: number | string
}
const searchText = ref('')
const rateCount = ref<number | string>('')
const preCount = ref<number | string>('')
const mobileLayout = useCollectionLayout(900)
const {
  detailsVisible, chartLoading, chartError,
  primaryChartElement, secondaryChartElement, showRecordDetails, resizeCharts, disposeCharts
} = useRecordCharts()
const {
  records: vitalRecords, total,
  pageSize, currentPage, load: loadVitals,
  handleSizeChange, changePage
} = usePagedRecords<VitalRow, { name: string; timeFirst: string; timeLast: string }>({
  path: 'api/v1/vitals/latest',
  responseKey: 'vitalRecords',
  buildQuery: () => ({ name: searchText.value, timeFirst: '', timeLast: '' })
})
const onlyPendingAlerts = ref(false)
const displayedVitals = computed(() => onlyPendingAlerts.value
  ? vitalRecords.value.filter(row => row.rateState == 1 || row.preState == 1)
  : vitalRecords.value)

function emptyAlert(): VitalAlertState {
  return {
    visible: false, loading: false, resolving: false, hasAlert: 1,
    title: '异常原因与处理', recordId: null,
    oldPersonName: '', hr: '', bp: '', rateBad: 0,
    preBad: 0, reason: '', detail: '', occurTime: '',
    advice: '', message: ''
  }
}
const alertDialog = ref<VitalAlertState>(emptyAlert())
const resolvingRow = ref(false)

async function loadAlertCount() {
  try {
    const { data } = await api.post<{ rateTotal: number; preTotal: number }>(
      'api/v1/vitals/alerts/count', {}
    )
    rateCount.value = data.rateTotal
    preCount.value = data.preTotal
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '请求未完成，请重试'))
  }
}
const getCount = loadAlertCount
const alertReads = createLatestRead(
  (query: { recordId?: number | string; oldId?: number | string; kind?: string }, signal: AbortSignal) =>
    api.post<VitalAlertDetail>('api/v1/vitals/alerts/detail', query, { signal }),
  ({ data }) => {
    Object.assign(alertDialog.value, {
      hasAlert: data.hasAlert ?? 1,
      message: data.message || '',
      recordId: data.recordId ?? null,
      oldPersonName: data.oldPersonName || alertDialog.value.oldPersonName,
      hr: data.hr ?? '', bp: data.bp ?? '',
      rateBad: data.rateBad || 0, preBad: data.preBad || 0,
      reason: data.reason || '', detail: data.detail || '',
      occurTime: data.occurTime || '', advice: data.advice || ''
    })
  },
  error => {
    alertDialog.value.hasAlert = 0
    alertDialog.value.message = apiErrorMessage(error, '读取失败，请稍后重试')
  },
  loading => { alertDialog.value.loading = loading }
)
function loadAlertDetail(
  recordId: number | string | null, name: string, oldId: number | string, kind = ''
) {
  alertDialog.value = { ...emptyAlert(), visible: true, loading: true, recordId, oldPersonName: name || '' }
  return alertReads.run(recordId !== null ? { recordId } : { oldId, kind })
}
function showAlertRow(row: VitalRow) {
  return loadAlertDetail(null, row.name, row.oldId)
}
function showAlertByKind(kind: string) {
  const target = vitalRecords.value.find(row =>
    kind === 'rate' ? row.rateState == 1 : row.preState == 1
  )
  if (!target) {
    ElMessage({ type: 'success', message: '当前没有未处理的' + (kind === 'rate' ? '心率' : '血压') + '异常' })
    return
  }
  void loadAlertDetail(null, target.name, target.oldId, kind)
}
async function resolveAlert(kind: string) {
  if (alertDialog.value.resolving) return
  alertDialog.value.resolving = true
  try {
    const { data } = await api.post<Envelope>('api/v1/vitals/alerts/resolve', {
      recordId: alertDialog.value.recordId, kind
    })
    accepted(data)
    ElMessage({ type: 'success', message: '已处理，红点已消除' })
    alertDialog.value.visible = false
    await Promise.all([loadVitals(true), getCount()])
  } catch (error) {
    ElMessage({ type: 'error', message: apiErrorMessage(error, '处理失败') })
  } finally { alertDialog.value.resolving = false }
}
async function resolveRow(row: VitalRow) {
  if (resolvingRow.value) return
  const kinds = [
    ...(row.rateState == 1 ? ['rate'] : []),
    ...(row.preState == 1 ? ['pre'] : [])
  ]
  if (!kinds.length) return
  resolvingRow.value = true
  try {
    const results = await Promise.allSettled(kinds.map(kind =>
      api.post<Envelope>('api/v1/vitals/alerts/resolve', { recordId: row.recordId, kind })
        .then(({ data }) => accepted(data))
    ))
    const failures = results.flatMap(result => result.status === 'rejected' ? [result.reason as unknown] : [])
    if (failures.some(error => ['timeout', 'network'].includes(transportOutcome(error)))) {
      ElMessage({ type: 'error', message: UNCONFIRMED_MESSAGE })
    } else if (failures.length) {
      ElMessage({ type: 'warning', message: apiErrorMessage(failures[0], '部分异常未处理完成') })
    } else {
      ElMessage({ type: 'success', message: '已处理，红点已消除' })
    }
    await Promise.all([loadVitals(true), getCount()])
  } finally { resolvingRow.value = false }
}
function togglePendingAlerts() {
  if (onlyPendingAlerts.value) {
    onlyPendingAlerts.value = false
    return
  }
  if (!vitalRecords.value.some(row => row.rateState == 1 || row.preState == 1)) {
    ElMessage({ type: 'info', message: '本页没有待处理异常' })
    return
  }
  onlyPendingAlerts.value = true
}
function showRoleDialog(row: VitalRow) {
  return showRecordDetails('vitals', row.oldId, row.name)
}
onMounted(() => { void loadAlertCount() })
onBeforeUnmount(alertReads.dispose)
</script>
