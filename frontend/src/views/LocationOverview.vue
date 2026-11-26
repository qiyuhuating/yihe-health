<template>
  <div class="roles">
    
    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="loadLocationRecords()">
		  
        <div class="search-field">
          
          <label class="table-search-label" for="search-position">长者姓名</label>
          <el-input id="search-position"
            placeholder="按长者姓名查找"
            v-model="searchText"
            clearable
            @clear="loadLocationRecords()"
            class="input-with-select">
            <template #append><el-button aria-label="查询长者姓名"
              
              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>
        
				<el-badge :value="num" class="item">
				  <el-button size="small">紧急告警</el-button>
				</el-badge>
				
      </form>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon></el-alert>
    <el-table v-if="!mobileLayout" class="adaptive-record-table" v-loading="isLoading" :data="positionList"
    style="width: 100%" border>
    <template #empty><div  class="position-empty">{{ loadError ? '位置记录暂时无法加载' : '暂无位置记录' }}</div></template>

      <el-table-column  width="100" type="index" align="center"></el-table-column>
      <el-table-column prop="name" label="姓名" width="140" align="center"></el-table-column>
      <el-table-column prop="sex" label="性别" width="100" align="center"></el-table-column>
	  <el-table-column prop="status" label="告警状态" width="150" align="center">
		  <template #default="scope" >
		    <el-tag v-if="scope.row.status == 1" type="danger" size="small">紧急告警</el-tag>
		    <el-tag v-else-if="scope.row.status == 0" type="success" size="small">未告警/已处理</el-tag>
        <span v-else class="unknown-state">状态未知</span>
		  </template>
	  </el-table-column>
	  <el-table-column prop="lng" label="经度" width="160" align="center"></el-table-column>
	  <el-table-column prop="lat" label="纬度" width="160" align="center"></el-table-column>
      <el-table-column label="定位来源与采样" width="240" align="left"><template #default="scope"><span class="position-context">{{ describePositionContext(scope.row) }}</span></template></el-table-column>
	  <el-table-column prop="beTime" label="上报时间" width="auto" align="center"></el-table-column>
      <el-table-column label="操作" align="center">
      	<template #default="scope" >
      		<el-button v-if="scope.row.status==1" size="small" plain type="danger" :icon="WarningFilled"
                       @click="showFallReason(scope.row)">告警记录与处理</el-button>
      		<el-button v-if="scope.row.status==1" size="small" plain type="primary" :icon="Check"
      		           @click="handleFall(scope.row)">标记已处理</el-button>
      		<el-button size="small" plain type="success" :icon="Check" @click="showRoleDialog(scope.row)">
      			详细情况
      		</el-button>
      	</template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout" :records="positionList" :fields='[{"key":"sex","label":"性别"},{"key":"status","label":"告警状态","state":"fall"},{"key":"lng","label":"经度"},{"key":"lat","label":"纬度"}]' label="位置与告警记录" title-key="name" key-field="oldId" time-key="beTime">
      <template #actions="scope"  ><span class="position-context">{{ describePositionContext(scope.row) }}</span><el-button v-if="scope.row.status==1" size="small" plain type="danger" :icon="WarningFilled"
                       @click="showFallReason(scope.row)">告警记录与处理</el-button>
<el-button v-if="scope.row.status==1" size="small" plain type="primary" :icon="Check"
      		           @click="handleFall(scope.row)">标记已处理</el-button>
<el-button size="small" plain type="success" :icon="Check" @click="showRoleDialog(scope.row)">
      			详细情况
      		</el-button></template>
    </record-cards>
    
    <div class="care-pagination-bar">
      <el-pagination layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleSizeChange"
        @current-change="changePage"
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total" />
    </div>
    <position-map :selection="selectedLocation || undefined" />
  </div>
	
</template>

<script setup lang="ts">
import { Check, Search, WarningFilled } from '@element-plus/icons-vue'
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { api, accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import store from '@/store'
import PositionMap from '@/components/PositionMap.vue'
import RecordCards from '@/components/RecordCards.vue'
import { describePositionContext } from '../utils/positionContext'
import { positionAlertMessage, type PositionAlarm } from './positionAlertMessage'

interface PositionRow extends PositionAlarm {
  oldId: number | string
  name: string
  sex?: string
  status?: number | string
  lng?: number | string | null
  lat?: number | string | null
  beTime?: string
}
const searchText = ref('')
const mobileLayout = useCollectionLayout(900)
const {
  records: positionList, isLoading, loadError, total,
  pageSize, currentPage, load: loadLocationRecords,
  handleSizeChange, changePage
} = usePagedRecords<PositionRow, { name: string }>({
  path: 'api/v1/position/select',
  responseKey: 'positionDtos',
  buildQuery: () => ({ name: searchText.value }),
  failure: '位置数据暂时无法加载，请检查服务连接后重试。'
})
const selectedLocation = ref<PositionRow | null>(null)
const num = computed(() => positionList.value.filter(row => row.status === 1 || row.status === '1').length)

watch(() => store.state.alertRevision, () => { void loadLocationRecords(true) })
watch(positionList, rows => {
  const id = store.state.selectedResidentId
  const selected = id == null ? null : rows.find(row => String(row.oldId) === id)
  if (selected) selectedLocation.value = { ...selected }
})
function showFallReason(row: PositionRow) {
  const { message, title } = positionAlertMessage(row)
  return ElMessageBox.alert(message, title, { confirmButtonText: '知道了' })
}
async function handleFall(row: PositionRow) {
  try {
    await ElMessageBox.confirm(
      '确认该紧急告警已经处理（联系过老人/家属）？处理后会从红点统计里移除。',
      '提示',
      { confirmButtonText: '确定已处理', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  try {
    const { data } = await api.post<Envelope>('api/v1/locations/alerts/resolve-latest', { oldId: row.oldId })
    accepted(data)
    ElMessage({ type: 'success', message: '已处理，告警红点已消除' })
    store.notifyAlertsChanged()
  } catch (error) {
    ElMessage({ type: 'error', message: apiErrorMessage(error, '处理失败') })
  }
}
function showRoleDialog(row: PositionRow) {
  selectedLocation.value = { ...row }
  store.selectResident(row.oldId)
}
</script>

<style scoped>
  .position-context { display: block; width: 100%; line-height: 1.6; font-size: 13px; color: var(--care-text-secondary); }
  .position-empty, .unknown-state { color: var(--care-text-muted); }

</style>
