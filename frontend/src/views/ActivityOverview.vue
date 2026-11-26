<template>
  <div class="roles">

    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="searchStep()">
        <div class="search-field">

          <label class="table-search-label" for="search-step">长者姓名</label>
          <el-input id="search-step"
            placeholder="按长者姓名查找"
            v-model="searchText"
            class="input-with-select">
            <template #append><el-button aria-label="查询长者姓名"

              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>

		<span class="record-summary" role="status">睡眠异常 <strong>{{ stepCount === '' ? '—' : stepCount }}</strong></span>
      </form>
    </div>

    <el-table v-if="!mobileLayout" class="adaptive-record-table" :data="stepList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无活动数据</strong><span>手表上报的步数与睡眠记录会显示在这里。</span></div></template>

      <el-table-column type="index" width="100" align="center"></el-table-column>
      <el-table-column prop="name" label="姓名" width="180" align="center"></el-table-column>
      <el-table-column prop="sex" label="性别" width="100" align="center"></el-table-column>
			<el-table-column prop="step" label="今日步数" width="150" align="center"></el-table-column>
			<el-table-column prop="sleep" label="睡眠时间" width="150" align="center"></el-table-column>
			<el-table-column prop="sleepAlertState" label="睡眠情况" width="150" align="center">
				<template #default="scope" >
				  <span v-if="scope.row.sleepAlertState==1" class="status-danger">异常</span>
				  <span v-else-if="scope.row.sleepAlertState === 0 || scope.row.sleepAlertState === '0'" class="status-normal">正常</span><span v-else class="status-unknown">暂无状态</span>
				</template>
			</el-table-column>
			<el-table-column prop="createTime" label="时间" width="180" align="center"></el-table-column>
      <el-table-column label="操作" align="center">
        <template #default="scope" >

          <el-button size="small" plain type="success" :icon="Check" aria-label="查看睡眠详情" @click="showRoleDialog(scope.row)">
            查看睡眠详情
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout" :records="stepList" :fields='[{"key":"sex","label":"性别"},{"key":"step","label":"今日步数"},{"key":"sleep","label":"睡眠时间"},{"key":"sleepAlertState","label":"睡眠情况","state":"health"}]' label="睡眠与步数记录" title-key="name" key-field="oldId" time-key="createTime">
      <template #actions="scope"  ><el-button size="small" plain type="success" :icon="Check" aria-label="查看睡眠详情" @click="showRoleDialog(scope.row)">
            查看睡眠详情
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

    <el-dialog title="睡眠记录详情" v-model="detailsVisible" @opened="resizeCharts" @closed="disposeCharts">
      <el-alert v-if="chartError" :title="chartError" type="warning" :closable="false" show-icon></el-alert>
      <div v-loading="chartLoading" class="record-chart-content"><div class="record-chart trend-chart" ref="primaryChartElement"></div></div>
      <template #footer><div  class="dialog-footer"><el-button type="primary" @click="detailsVisible = false">关闭详情</el-button></div></template>
    </el-dialog>
  </div>

</template>

<script setup lang="ts">
import { Check, Search } from '@element-plus/icons-vue'
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { useRecordCharts } from '@/composables/useRecordCharts'
import { api as requestClient } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import RecordCards from '@/components/RecordCards.vue'

const searchText = ref('')
const stepCount = ref<string | number>('')
const mobileLayout = useCollectionLayout(900)
const {
  detailsVisible, chartLoading, chartError,
  primaryChartElement, showRecordDetails, resizeCharts, disposeCharts
} = useRecordCharts()
interface ActivityRecord {
  oldId: number | string
  name: string
}
const {
  records: stepList, total,
  pageSize, currentPage, load: searchStep,
  handleSizeChange, changePage
} = usePagedRecords<ActivityRecord>({
  path: 'api/v1/activity/latest',
  responseKey: 'activityRecords',
  buildQuery: () => ({ name: searchText.value, timeFirst: '', timeLast: '' })
})
async function getCount() {
  try {
    const { data } = await requestClient.post<{ sleep: number | string }>('api/v1/activity/alerts/device-counts', {})
    stepCount.value = data.sleep
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '请求未完成，请重试'))
  }
}
function showRoleDialog(row: ActivityRecord) {
  return showRecordDetails('activity', row.oldId, row.name)
}
onMounted(() => { void getCount() })
</script>
