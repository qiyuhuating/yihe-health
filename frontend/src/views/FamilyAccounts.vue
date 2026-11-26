<template>
  <div class="roles" v-loading="isLoading" element-loading-text="正在加载家属账号…" :aria-busy="isLoading">
    
    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="loadFamilyAccounts()">
        <div class="search-field">
          
          <label class="table-search-label" for="search-family">家属姓名</label>
          <el-input id="search-family"
            placeholder="按家属姓名查找"
            v-model="searchText"
            class="input-with-select">
            <template #append><el-button aria-label="查询家属姓名"
              
              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>
        <div class="toolbar-actions">
          <el-button plain @click="exportCurrentPage">导出本页</el-button>
          <el-button plain @click="createDialogVisible = true">添加家属</el-button>
        </div>
		
      </form>
    </div>
    
    <el-dialog :close-on-click-modal="false"
      title="家属添加"
      v-model="createDialogVisible"
      @close="resetCreateForm"
      width="92%" class="admin-record-dialog">
      <FamilyForm :draft="familyForm" :rules="rules" ref="createFormRef" />
      <template #footer><span  class="dialog-footer">
      <el-button @click="createDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="addFamily()">确 定</el-button>
    </span></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false"
      title="家属编辑"
      v-model="editDialogVisible"
      width="92%" class="admin-record-dialog">
      <FamilyForm editing :draft="familyEditForm" :rules="rules" />
      <template #footer><span  class="dialog-footer">
      <el-button @click="editDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="editFamily">确 定</el-button>
    </span></template>
    </el-dialog>

    <div v-if="loadError" class="collection-feedback" role="alert"><span>{{ loadError }}</span><el-button plain :loading="isLoading" @click="loadFamilyAccounts(true)">重新加载</el-button></div>
    <el-table v-if="!mobileLayout && !loadError" class="adaptive-record-table" :data="familyList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无家属账号</strong><span>添加家属或调整搜索条件后，账号会显示在这里。</span></div></template>

      <el-table-column type="index" width="100" align="center"></el-table-column>
	  <el-table-column prop="name" label="姓名" width="140" align="center"></el-table-column>
	  <el-table-column prop="oldId" label="老人编号" width="140" align="center"></el-table-column>
      <el-table-column prop="familyName" label="家属姓名" width="140" align="center"></el-table-column>
      <el-table-column prop="familyRelation" label="关系" width="140" align="center"></el-table-column>
	  <el-table-column prop="familySex" label="性别" width="140" align="center"></el-table-column>
	  <el-table-column prop="familyPhone" label="电话" width="140" align="center"></el-table-column>
	  <el-table-column prop="familyAddress" label="地址" width="auto" align="center"></el-table-column>
      <el-table-column label="操作" align="center">
        <template #default="scope" >
          <el-button size="small" plain type="primary" :icon="Edit"
                     @click="openFamilyEdit(scope.$index, scope.row)">编辑
          </el-button>
          <el-button size="small" plain type="danger" :icon="Delete" @click="deleteFamily(scope.row.familyId)">删除
          </el-button>
          
        </template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout && !loadError" :records="familyList" :fields='[{"key":"name","label":"姓名"},{"key":"oldId","label":"老人编号"},{"key":"familyRelation","label":"关系"},{"key":"familySex","label":"性别"},{"key":"familyPhone","label":"电话"},{"key":"familyAddress","label":"地址"}]' label="家属账号资料" title-key="familyName" key-field="familyId">
      <template #actions="scope"  ><el-button size="small" plain type="primary" :icon="Edit"
                     @click="openFamilyEdit(scope.$index, scope.row)">编辑
          </el-button>
<el-button size="small" plain type="danger" :icon="Delete" @click="deleteFamily(scope.row.familyId)">删除
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
import { ElMessage } from 'element-plus'
import { usePagedRecords } from '@/composables/usePagedRecords'
import { writeRecord, deleteRecord } from '@/composables/recordActions'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { downloadCsv } from '../utils/csv'
import RecordCards from '@/components/RecordCards.vue'
import FamilyForm from '@/components/records/FamilyForm.vue'

interface FamilyRow extends Record<string, unknown> {
  familyId?: number | string
  oldId?: number | string
  name?: string
  familyName?: string
  familyRelation?: string
  familySex?: string
  familyPhone?: string
  familyAddress?: string
}
const searchText = ref('')
const mobileLayout = useCollectionLayout(900)
const {
  records: familyList, isLoading, loadError, total,
  pageSize, currentPage, load: loadFamilyAccounts,
  handleSizeChange, changePage
} = usePagedRecords<FamilyRow, { familyName: string }>({
  path: 'api/v1/family/select',
  responseKey: 'familyDtos',
  buildQuery: () => ({ familyName: searchText.value }),
  failure: '家属账号暂时无法加载，请检查服务连接后重试。'
})
const createDialogVisible = ref(false)
const editDialogVisible = ref(false)
const familyForm = reactive({
  familyName: '', familyRelation: '', familySex: '',
  familyPhone: '', familyAddress: '', oldPhone: ''
})
const familyEditForm = ref<FamilyRow>({})
const createFormRef = ref<{ resetFields: () => void } | null>(null)
function checkPhone(_rule: unknown, value: string, callback: (error?: Error) => void) {
  if (!value) return callback(new Error('电话号码不能为空'))
  if (!/^1[3-9]\d{9}$/.test(String(value))) return callback(new Error('电话号码格式不正确'))
  callback()
}
const rules = {
  familyPhone: [{ validator: checkPhone, trigger: 'blur' }],
  oldPhone: [{ validator: checkPhone, trigger: 'blur' }]
}
function addFamily() {
  return writeRecord(
    'api/v1/family/add', familyForm,
    '家属已添加，请生成该老人的邀请码，由家属注册并设置登录密码。',
    async () => {
      createDialogVisible.value = false
      await loadFamilyAccounts(true)
    },
    '添加家属失败，请检查服务连接后重试。'
  )
}
function resetCreateForm() { createFormRef.value?.resetFields() }
function editFamily() {
  const { familyId, familyName, familyRelation, familySex, familyPhone, familyAddress } = familyEditForm.value
  if (familyId == null) return
  const body = { familyId, familyName, familyRelation, familySex, familyPhone, familyAddress }
  return writeRecord('api/v1/family/updateFamily', body, '家属信息已保存', async () => {
    editDialogVisible.value = false
    await loadFamilyAccounts(true)
  })
}
function deleteFamily(familyId: number | string | undefined) {
  if (familyId === undefined) return
  return deleteRecord('api/v1/family/deleteFamily', { familyId },
    '确认删除该家属账号？', () => loadFamilyAccounts(true))
}
function openFamilyEdit(_index: number, row: FamilyRow) {
  familyEditForm.value = { ...row }
  editDialogVisible.value = true
}
function exportCurrentPage() {
  if (!familyList.value.length) { ElMessage.warning('当前没有可导出的数据'); return }
  downloadCsv(`家属账号-${new Date().toISOString().slice(0, 10)}.csv`, [
    { key: 'name', label: '老人姓名' },
    { key: 'oldId', label: '老人编号' },
    { key: 'familyName', label: '家属姓名' },
    { key: 'familyRelation', label: '关系' },
    { key: 'familySex', label: '性别' },
    { key: 'familyPhone', label: '电话' },
    { key: 'familyAddress', label: '地址' }
  ], familyList.value)
}
</script>
