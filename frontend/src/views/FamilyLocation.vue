<template>
  <div class="roles">
    <div class="record-heading"><h2>最近上报记录</h2><el-button link :loading="isLoading" @click="loadFamilyLocations">刷新记录 <el-icon aria-hidden="true"><Refresh /></el-icon></el-button></div>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon></el-alert>
    <el-table v-if="!mobileLayout" class="desktop-records" v-loading="isLoading" :data="positionList"
    style="width: 100%" border>
    <template #empty><div  class="position-empty">{{ loadError ? '位置记录暂时无法加载' : '暂无位置记录' }}</div></template>

	  <el-table-column prop="name" label="长者" width="150" align="center"></el-table-column>
	  <el-table-column prop="status" label="安全状态" width="200" align="center">
		  <template #default="scope" >
		    <el-tag v-if="scope.row.status == 1" type="danger" size="small">紧急告警</el-tag>
		    <el-tag v-else-if="scope.row.status == 0" type="success" size="small">未告警/已处理</el-tag>
        <span v-else class="unknown-state">状态未知</span>
		  </template>
	  </el-table-column>
	  <el-table-column label="位置坐标" width="220" align="center"><template #default="scope" >{{ scope.row.lng }}，{{ scope.row.lat }}</template></el-table-column>
      <el-table-column label="定位来源与采样" width="240"><template #default="scope">{{ describePositionContext(scope.row) }}</template></el-table-column>
	  <el-table-column prop="beTime" label="记录时间" width="190" align="center"></el-table-column>
      <el-table-column label="查看" width="170" align="center">
      	<template #default="scope" >

        <el-button size="small" plain type="success" @click="showLocation(scope.row)">
          地图查看
        </el-button>
      	</template>
      </el-table-column>
    </el-table>
    <section v-if="mobileLayout && positionList.length" class="family-record-cards" aria-label="位置和安全记录">
      <article v-for="item in positionList" :key="item.oldId" class="family-record-card">
        <h2>{{ item.name || '位置记录' }}</h2>
        <dl>
          <dt>安全状态</dt>
          <dd :class="item.status == 1 ? 'status-danger' : item.status == 0 ? 'status-normal' : ''">{{ item.status == 1 ? '紧急告警，请及时确认' : item.status == 0 ? '未告警/已处理' : '状态未知' }}</dd>
          <dt>记录时间</dt><dd>{{ item.beTime || '暂无时间信息' }}</dd>
          <dt>位置坐标</dt><dd>{{ item.lng }}, {{ item.lat }}</dd>
          <dt>定位来源与采样</dt><dd>{{ describePositionContext(item) }}</dd>
        </dl>
        <el-button type="primary" @click="showLocation(item)">在地图中查看位置</el-button>
      </article>
    </section>
    <div v-else-if="mobileLayout && !isLoading" class="family-record-empty">{{ loadError || '暂时没有位置和安全记录。' }}</div>
    <position-map :selection="selectedLocation" />
  </div>

</template>

<script setup lang="ts">
import { Refresh } from '@element-plus/icons-vue'
import { computed, ref, shallowRef, onMounted, onBeforeUnmount } from 'vue'
import { authState } from '../auth'
import { api } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { createLatestRead } from '../composables/latestRead'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import PositionMap from '@/components/PositionMap.vue'
import { describePositionContext, type PositionContext } from '../utils/positionContext'

const mobileLayout = useCollectionLayout()
const oldId = computed(() => String(authState.account?.oldId ?? ''))
interface PositionRecord extends PositionContext {
  oldId?: string | number
  name: string
  lng: number | string
  lat: number | string
  status?: string | number | null
  beTime?: string
}
const positionList = shallowRef<PositionRecord[]>([])
const isLoading = ref(false)
const loadError = ref('')
const selectedLocation = ref<PositionRecord | null>(null)
const reads = createLatestRead(
  (query: { oldId: string }, signal: AbortSignal) => api.post<{ positionDtos?: PositionRecord[] }>('api/v1/position/selectFamily', query, { signal }),
  ({ data }) => {
    positionList.value = Array.isArray(data.positionDtos) ? data.positionDtos : []
    loadError.value = ''
  },
  error => {
    positionList.value = []
    loadError.value = apiErrorMessage(error, '位置记录暂时无法加载，请检查服务连接后重试。')
  },
  loading => { isLoading.value = loading }
)
function loadFamilyLocations() { return reads.run({ oldId: oldId.value }) }
function showLocation(row: PositionRecord) { selectedLocation.value = { ...row } }
onMounted(() => { void loadFamilyLocations() })
onBeforeUnmount(reads.dispose)
</script>

<style scoped>
  .position-empty, .unknown-state { color: var(--care-text-muted); font-size: 16px; }

</style>
