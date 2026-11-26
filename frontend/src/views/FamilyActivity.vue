<template>
  <div class="roles">
    <div class="record-heading"><h2>最近上报记录</h2><el-button link :loading="isLoading" @click="searchStep">刷新记录 <el-icon aria-hidden="true"><Refresh /></el-icon></el-button></div>
    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon></el-alert>

    <el-table v-if="!mobileLayout" v-loading="isLoading" class="desktop-records" :data="stepList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无活动数据</strong><span>家人手表上报的步数与睡眠记录会显示在这里。</span></div></template>

			<el-table-column prop="name" label="长者" width="160" align="center"></el-table-column>
			<el-table-column prop="step" label="今日步数" width="145" align="center"></el-table-column>
			<el-table-column prop="sleep" label="睡眠时长" width="155" align="center"></el-table-column>
			<el-table-column prop="sleepAlertState" label="睡眠情况" width="165" align="center">
				<template #default="scope" >
				  <span :class="stateClass(scope.row.sleepAlertState)">{{ stateLabel(scope.row.sleepAlertState) }}</span>
				</template>
			</el-table-column>
			<el-table-column prop="createTime" label="记录时间" width="190" align="center"></el-table-column>
      <el-table-column label="查看" width="180" align="center">
        <template #default="scope" >

          <el-button size="small" plain type="success" :icon="Check" @click="showDetails(scope.row)">
            详细情况
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <section v-if="mobileLayout && stepList.length" class="family-record-cards" aria-label="活动和睡眠记录">
      <article v-for="item in stepList" :key="item.oldId" class="family-record-card">
        <h2>{{ item.name || '活动记录' }}</h2>
        <dl>
          <dt>今日步数</dt><dd>{{ item.step == null ? '暂无数据' : item.step + ' 步' }}</dd>
          <dt>睡眠时长</dt><dd>{{ item.sleep == null ? '暂无数据' : item.sleep }}</dd>
          <dt>睡眠情况</dt><dd :class="stateClass(item.sleepAlertState)">{{ stateLabel(item.sleepAlertState) }}{{ item.sleepAlertState == 1 ? '，请查看详情' : '' }}</dd>
          <dt>记录时间</dt><dd>{{ item.createTime || '暂无时间信息' }}</dd>
        </dl>
        <el-button type="primary" @click="showDetails(item)">查看睡眠详情</el-button>
      </article>
    </section>
    <div v-else-if="mobileLayout" class="family-record-empty">{{ isLoading ? '正在加载活动记录…' : loadError || '暂时没有活动或睡眠记录。' }}</div>

    <el-dialog :title="chartName ? chartName + '的睡眠详情' : '睡眠详情'" width="92%" v-model="detailsVisible" @opened="resizeCharts" @closed="disposeCharts">
      <el-alert v-if="chartError" :title="chartError" type="warning" :closable="false" show-icon></el-alert><el-button v-if="chartError" link @click="showDetails({ name: chartName })">重新加载详情</el-button><div v-loading="chartLoading">

	  <div v-show="!chartError" class="sleep-chart trend-chart" ref="primaryChartElement"></div>

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
  primaryChartElement, showRecordDetails, resizeCharts, disposeCharts
} = useRecordCharts()
const oldId = computed(() => String(authState.account?.oldId ?? ''))
interface ActivityRecord {
  oldId?: string | number
  name: string
  step?: number | null
  sleep?: string | number | null
  sleepAlertState?: string | number | null
  createTime?: string
}
const stepList = shallowRef<ActivityRecord[]>([])
const isLoading = ref(false)
const loadError = ref('')
const reads = createLatestRead(
  (query: { oldId: string }, signal: AbortSignal) => api.post<{ activityRecords?: ActivityRecord[] }>('api/v1/activity/history', query, { signal }),
  ({ data }) => {
    stepList.value = Array.isArray(data.activityRecords) ? data.activityRecords : []
    loadError.value = ''
  },
  error => {
    stepList.value = []
    loadError.value = apiErrorMessage(error, '记录暂时无法加载，请点击刷新重试。')
  },
  loading => { isLoading.value = loading }
)
function searchStep() { return reads.run({ oldId: oldId.value }) }
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
function showDetails(row: Pick<ActivityRecord, 'name'>) {
  return showRecordDetails('activity', oldId.value, row.name)
}
onMounted(() => { void searchStep() })
onBeforeUnmount(reads.dispose)
</script>

<style scoped>

  .sleep-chart { width: 100%; max-width: 520px; height: 300px; margin: 0 auto; }

</style>
