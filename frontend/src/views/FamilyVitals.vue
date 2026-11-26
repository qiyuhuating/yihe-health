<template>
  <div class="roles">
    <div class="record-heading"><h2>最近上报记录</h2><el-button link :loading="isLoading" @click="searchOlds">刷新记录 <el-icon aria-hidden="true"><Refresh /></el-icon></el-button></div>
    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon></el-alert>

    <el-table v-if="!mobileLayout" v-loading="isLoading" class="desktop-records" :data="oldsList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无健康记录</strong><span>老人的最新心率与血压状态会显示在这里。</span></div></template>

			<el-table-column prop="name" label="长者" width="180" align="center"></el-table-column>
			<el-table-column prop="preState" label="血压状态" width="170" align="center">
				<template #default="scope" >
                  <span :class="stateClass(scope.row.preState)">{{ stateLabel(scope.row.preState) }}</span>
				</template>
			</el-table-column>
			<el-table-column prop="rateState" label="心率状态" width="170" align="center">
				<template #default="scope" >
					<span :class="stateClass(scope.row.rateState)">{{ stateLabel(scope.row.rateState) }}</span>
				</template>
			</el-table-column>
			<el-table-column prop="createTime" label="记录时间" width="210" align="center"></el-table-column>
      <el-table-column label="查看" width="190" align="center">
        <template #default="scope" >

          <el-button size="small" plain type="success" :icon="Check" @click="showDetails(scope.row)">
            详细情况
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <section v-if="mobileLayout && oldsList.length" class="family-record-cards" aria-label="心率与血压记录">
      <article v-for="item in oldsList" :key="item.oldId" class="family-record-card">
        <h2>{{ item.name || '健康记录' }}</h2>
        <dl>
          <dt>血压状态</dt><dd :class="stateClass(item.preState)">{{ stateLabel(item.preState) }}{{ item.preState == 1 ? '，请查看详情' : '' }}</dd>
          <dt>心率状态</dt><dd :class="stateClass(item.rateState)">{{ stateLabel(item.rateState) }}{{ item.rateState == 1 ? '，请查看详情' : '' }}</dd>
          <dt>记录时间</dt><dd>{{ item.createTime || '暂无时间信息' }}</dd>
        </dl>
        <el-button type="primary" @click="showDetails(item)">查看心率和血压详情</el-button>
      </article>
    </section>
    <div v-else-if="mobileLayout" class="family-record-empty">{{ isLoading ? '正在加载健康记录…' : loadError || '暂时没有心率或血压记录。' }}</div>

    <el-dialog :title="chartName ? chartName + '的健康详情' : '心率与血压详情'" width="92%" v-model="detailsVisible" @opened="resizeCharts" @closed="disposeCharts" >
      <el-alert v-if="chartError" :title="chartError" type="warning" :closable="false" show-icon></el-alert><el-button v-if="chartError" link @click="showDetails({ name: chartName })">重新加载详情</el-button><div v-loading="chartLoading" class="health-chart-grid">

	  <div v-show="!chartError" class="rate-chart trend-chart" ref="primaryChartElement"></div>

		<div v-show="!chartError" class="rate-chart trend-chart" ref="secondaryChartElement"></div>

      </div>
      <template #footer><div  class="dialog-footer">
        <el-button type="primary" @click="detailsVisible = false">关闭详情</el-button>
      </div></template>
    </el-dialog>
  </div>

</template>

<script setup lang="ts">
import { Check, Refresh } from '@element-plus/icons-vue'
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { authState } from '../auth'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { useRecordCharts } from '@/composables/useRecordCharts'
import { createLatestRead } from '@/composables/latestRead'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'

const mobileLayout = useCollectionLayout()
const {
  detailsVisible, chartLoading, chartError, chartName,
  primaryChartElement, secondaryChartElement, showRecordDetails, resizeCharts, disposeCharts
} = useRecordCharts()
const oldId = computed(() => String(authState.account?.oldId ?? ''))
interface VitalRecord {
  oldId?: string | number
  name: string
  rateState?: string | number | null
  preState?: string | number | null
  createTime?: string
}
const oldsList = shallowRef<VitalRecord[]>([])
const isLoading = ref(false)
const loadError = ref('')
const reads = createLatestRead(
  (query: { oldId: string }, signal: AbortSignal) => api.post<{ vitalRecords?: VitalRecord[] }>('api/v1/vitals/history', query, { signal }),
  ({ data }) => {
    oldsList.value = Array.isArray(data.vitalRecords) ? data.vitalRecords : []
    loadError.value = ''
  },
  error => {
    oldsList.value = []
    loadError.value = apiErrorMessage(error, '记录暂时无法加载，请点击刷新重试。')
  },
  loading => { isLoading.value = loading }
)
function searchOlds() { return reads.run({ oldId: oldId.value }) }
function stateLabel(value: string | number | null | undefined) {
  if (value === 1 || value === '1') return '异常'
  if (value === 0 || value === '0') return '正常'
  return '暂无状态'
}
function stateClass(value: string | number | null | undefined) {
  if (value === 1 || value === '1') return 'status-danger'
  if (value === 0 || value === '0') return 'status-normal'
  return 'unknown-state'
}
function showDetails(row: Pick<VitalRecord, 'name'>) {
  return showRecordDetails('vitals', oldId.value, row.name)
}
onMounted(() => { void searchOlds() })
onBeforeUnmount(reads.dispose)
</script>

<style scoped>
.health-chart-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.rate-chart { width: 100%; min-width: 0; height: 320px; border: 1px solid var(--care-border-soft); border-radius: 12px; background: #fbfcf8; }
@media (max-width: 760px) { .health-chart-grid { grid-template-columns: 1fr; gap: 16px; } .rate-chart { height: 280px; } }
</style>
