<template>
  <div class="roles" v-loading="isLoading" element-loading-text="正在加载老人档案…" :aria-busy="isLoading">
    
    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="loadResidents()">
        <div class="search-field">
          
          <label class="table-search-label" for="search-person">长者姓名</label>
          <el-input id="search-person"
            placeholder="按长者姓名查找"
            v-model="searchText"
            class="input-with-select">
            <template #append><el-button aria-label="查询长者姓名"
              
              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>
        <div class="toolbar-actions">
          <el-button plain @click="exportCurrentPage">导出本页</el-button>
          <el-button plain @click="createDialogVisible = true">添加老人</el-button>
        </div>
		
      </form>
    </div>
    
    <el-dialog :close-on-click-modal="false"
      title="老人添加"
      v-model="createDialogVisible"
      @open="loadGuardianOptions"
      @close="resetCreateForm">
      <ResidentForm :draft="createForm" :rules="rules" ref="createFormRef" :options="guardianOptions" :customer-loading="guardianLoading" :customer-error="guardianError" @retry-guardians="loadGuardianOptions" />
      <template #footer><span  class="dialog-footer">
      <el-button @click="createDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="createResident()">确 定</el-button>
    </span></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false"
      title="老人编辑"
      v-model="editDialogVisible"
      @open="loadGuardianOptions">
      <ResidentForm editing :draft="editForm" :rules="rules" ref="editFormRef" :options="guardianOptions" :customer-loading="guardianLoading" :customer-error="guardianError" @retry-guardians="loadGuardianOptions" />
      <template #footer><span  class="dialog-footer">
      <el-button @click="editDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="updateResident">确 定</el-button>
    </span></template>
    </el-dialog>

    <div v-if="loadError" class="collection-feedback" role="alert"><span>{{ loadError }}</span><el-button plain :loading="isLoading" @click="loadResidents(true)">重新加载</el-button></div>
    <el-table v-if="!mobileLayout && !loadError" class="adaptive-record-table" :data="residentList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无老人档案</strong><span>添加老人或调整搜索条件后，档案会显示在这里。</span></div></template>

	<el-table-column type="expand">
			  <template #default="prop" >
				<p>家属姓名:{{prop.row.familyName}}</p><p>家属性别:{{prop.row.familySex}}</p>
				<p>关系:{{prop.row.familyRelation}}</p><p>家属电话:{{prop.row.familyPhone}}</p>
				<p>家属地址:{{prop.row.familyAddress}}</p><p>过敏:{{prop.row.warn}}</p>
				<p>喜好食物:{{prop.row.foodLike}}</p><p>忌口食物:{{prop.row.foodHate}}</p>
				<p>常用药物:{{prop.row.drag}}</p><p>病史:{{prop.row.illness}}</p>
				
			  </template>
	  
	</el-table-column>
      
      <el-table-column type="index" width="100" align="center"></el-table-column>
      <el-table-column prop="name" label="姓名" width="100" align="center"></el-table-column>
      <el-table-column prop="bothdate" label="出生日期" width="120" align="center"></el-table-column>
	  <el-table-column prop="sex" label="性别" width="100" align="center"></el-table-column>
	  <el-table-column prop="phone" label="电话" width="120" align="center"></el-table-column>
	  <el-table-column prop="room" label="房间" width="100" align="center"></el-table-column>
	  <el-table-column prop="beTime" label="入院时间" width="100" align="center"></el-table-column>
	  
		<el-table-column prop="customerName" label="监护人" width="100" align="center"></el-table-column>
		<el-table-column prop="deviceId" label="手表编号" width="100" align="center"></el-table-column>

      <el-table-column label="操作" width="200" align="center" fixed="right">
        <template #default="scope" >
          <el-button size="small" plain type="primary" :icon="Edit"
                     @click="openResidentEdit(scope.$index, scope.row)">编辑
          </el-button>
          <el-button size="small" plain type="danger" :icon="Delete" @click="deleteResident(scope.row.oldId)">删除
          </el-button>
          
        </template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout && !loadError" :records="residentList" :fields='[{"key":"bothdate","label":"出生日期"},{"key":"sex","label":"性别"},{"key":"phone","label":"电话"},{"key":"room","label":"房间"},{"key":"beTime","label":"入院时间"},{"key":"familyName","label":"家属姓名"},{"key":"familyRelation","label":"关系"},{"key":"familyPhone","label":"家属电话"},{"key":"familyAddress","label":"家属地址"},{"key":"familySex","label":"家属性别"},{"key":"customerName","label":"监护人"},{"key":"deviceId","label":"手表编号"},{"key":"illness","label":"病史"},{"key":"warn","label":"过敏"},{"key":"foodLike","label":"喜好食物"},{"key":"foodHate","label":"忌口食物"},{"key":"drag","label":"常用药物"}]' label="老人档案" empty-title="暂无老人档案" title-key="name" key-field="oldId">
      <template #actions="scope"  ><el-button size="small" plain type="primary" :icon="Edit"
                     @click="openResidentEdit(scope.$index, scope.row)">编辑
          </el-button>
<el-button size="small" plain type="danger" :icon="Delete" @click="deleteResident(scope.row.oldId)">删除
          </el-button></template>
    </record-cards>
    
    <div v-if="!loadError" style="margin-top: 10px;text-align: center">
      <el-pagination layout="total, sizes, prev, pager, next, jumper"
        @size-change="handleSizeChange"
        @current-change="changePage"
        v-model:current-page="currentPage"
        :page-size="pageSize"
        :total="total" />
    </div>
  </div>

</template>
<script setup lang="ts">
import { Delete, Edit, Search } from '@element-plus/icons-vue'
import { reactive, ref } from 'vue'
import { ElMessage, type FormItemRule } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { writeRecord, deleteRecord } from '@/composables/recordActions'
import { editableResident } from '@/composables/residentFields'
import { api as requestClient } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import { downloadCsv } from '../utils/csv'
import RecordCards from '@/components/RecordCards.vue'
import ResidentForm from '@/components/records/ResidentForm.vue'

interface ResidentRecord extends Record<string, unknown> {
  oldId: string | number
  name: string
  customerName?: string
  deviceId?: string
}
interface GuardianOption extends Record<string, unknown> {
  customerId: string | number
  customerName: string
}
const searchText = ref('')
const mobileLayout = useCollectionLayout(900)
const {
  records: residentList, isLoading, loadError, total, pageSize, currentPage,
  load: loadResidents, handleSizeChange, changePage
} = usePagedRecords<ResidentRecord>({
  path: 'api/v1/residents/details',
  responseKey: 'oldPersonDetails',
  buildQuery: () => ({ name: searchText.value }),
  failure: '老人档案暂时无法加载，请检查服务连接后重试。'
})

const emptyResident = () => ({
  name: '', bothdate: '', sex: '', phone: '', room: '',
  beTime: '', customerId: '', illness: '', warn: '',
  foodLike: '', foodHate: '', drag: ''
})
const createDialogVisible = ref(false)
const editDialogVisible = ref(false)
const createForm = reactive(emptyResident())
const editForm = ref<ReturnType<typeof emptyResident> & { oldId: string | number; customerName: string; deviceId: string }>({ ...emptyResident(), oldId: '', customerName: '', deviceId: '' })
const createFormRef = ref<InstanceType<typeof ResidentForm> | null>(null)
const editFormRef = ref<InstanceType<typeof ResidentForm> | null>(null)
const guardianOptions = ref<GuardianOption[]>([])
const guardianLoading = ref(false)
const guardianError = ref('')

const checkPhone: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (!value) return callback(new Error('电话号码不能为空'))
  if (!/^1[3-9]\d{9}$/.test(String(value))) return callback(new Error('电话号码格式不正确'))
  callback()
}
const rules = { phone: [{ validator: checkPhone, trigger: 'blur' }] }

async function loadGuardianOptions() {
  if (guardianLoading.value) return
  guardianLoading.value = true
  guardianError.value = ''
  try {
    const residents: GuardianOption[] = []
    let page = 1
    let total = 0
    do {
      const { data } = await requestClient.post<{ customers: GuardianOption[]; total?: number }>('api/v1/guardians/list', { customerName: '', page, pageSize: 100 })
      if (!Array.isArray(data.customers)) throw new Error('invalid guardians')
      total = data.total ?? data.customers.length
      if (!Number.isInteger(total) || total < 0 || (!data.customers.length && residents.length < total)) {
        throw new Error('incomplete guardians')
      }
      residents.push(...data.customers)
      page += 1
    } while (residents.length < total)
    guardianOptions.value = residents
  } catch (error) {
    guardianOptions.value = []
    guardianError.value = apiErrorMessage(error, '监护人选项暂时无法加载，请重试。')
  } finally {
    guardianLoading.value = false
  }
}

function createResident() {
  createFormRef.value?.validate((valid: boolean) => {
    if (!valid) return
    void writeRecord('api/v1/residents/create', editableResident(createForm), '添加老人成功', async () => {
      createDialogVisible.value = false
      await loadResidents(true)
    })
  })
}
function updateResident() {
  editFormRef.value?.validate((valid: boolean) => {
    if (!valid) return
    const draft = editForm.value
    void writeRecord('api/v1/residents/update', {
      ...editableResident(draft), oldId: draft.oldId
    }, '编辑老人信息成功', async () => {
      editDialogVisible.value = false
      await loadResidents(true)
    })
  })
}
function deleteResident(oldId: number | string) {
  return deleteRecord('api/v1/residents/delete', { oldId }, '此操作将永久删除该长者信息，是否继续？', () => loadResidents(true))
}
function resetCreateForm() {
  createFormRef.value?.resetFields()
}
function openResidentEdit(_index: number, row: ResidentRecord) {
  editForm.value = { ...emptyResident(), ...row, oldId: row.oldId, customerName: row.customerName || '', deviceId: row.deviceId || '' }
  editDialogVisible.value = true
}
function exportCurrentPage() {
  if (!residentList.value.length) { ElMessage.warning('当前没有可导出的数据'); return }
  downloadCsv(`老人档案-${new Date().toISOString().slice(0, 10)}.csv`, [
    { key: 'name', label: '姓名' },
    { key: 'bothdate', label: '出生日期' },
    { key: 'sex', label: '性别' },
    { key: 'phone', label: '电话' },
    { key: 'room', label: '房间' },
    { key: 'beTime', label: '入院时间' },
    { key: 'customerName', label: '监护人' },
    { key: 'deviceId', label: '手表编号' }
  ], residentList.value)
}
</script>
