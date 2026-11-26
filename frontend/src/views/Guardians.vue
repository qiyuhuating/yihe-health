<template>
  <div class="roles" :aria-busy="isLoading">
    
    <div class="sys-research">
      <form class="row-search" role="search" @submit.prevent="loadGuardians()">
        <div class="search-field">
          
          <label class="table-search-label" for="search-customer">监护人姓名</label>
          <el-input id="search-customer"
            placeholder="按监护人姓名查找"
            v-model="searchText"
            class="input-with-select">
            <template #append><el-button aria-label="查询监护人姓名"
              
              :icon="Search"
              native-type="submit"></el-button></template>
          </el-input>
        </div>
        <div class="toolbar-actions">
          <el-button plain @click="exportCurrentPage">导出本页</el-button>
          <el-button plain @click="createDialogVisible = true">添加监护人</el-button>
        </div>
		
      </form>
    </div>
    
    <el-dialog :close-on-click-modal="false"
      title="监护人添加"
      v-model="createDialogVisible"
      @close="resetCreateForm">
      <el-form label-width="80px" :model="createForm" :rules="rules"  ref="createGuardianFormRef">
        <el-form-item label="姓名" prop="customerName">
          <el-input v-model="createForm.customerName"></el-input>
        </el-form-item>
        <el-form-item label="年龄" prop="customerAge">
          <el-input v-model="createForm.customerAge"></el-input>
        </el-form-item>
		<el-form-item label="性别" prop="customerSex">
		<el-radio-group v-model="createForm.customerSex">
		  <el-radio value="男">男</el-radio>
		  <el-radio value="女">女</el-radio>
		</el-radio-group>
		</el-form-item>
		<el-form-item label="电话" prop="customerPhone">
		<el-input v-model="createForm.customerPhone"></el-input>
		</el-form-item>
		<el-form-item label="地址" prop="customerAddress">
		<el-input v-model="createForm.customerAddress"></el-input>
		</el-form-item>
		
      </el-form>
      <template #footer><span  class="dialog-footer">
      <el-button @click="createDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="createGuardian()" :loading="saving">确 定</el-button>
    </span></template>
    </el-dialog>

    <el-dialog :close-on-click-modal="false"
      title="监护人编辑"
      v-model="editDialogVisible">
      <el-form ref="editGuardianFormRef" label-width="80px" :rules="rules" :model="editForm">
        <el-form-item label="姓名" prop="customerName">
        <el-input v-model="editForm.customerName"></el-input>
        </el-form-item>
				<el-form-item label="姓名" prop="customerId" style="display: none;">
				<el-input v-model="editForm.customerId"></el-input>
				</el-form-item>
        <el-form-item label="年龄" prop="customerAge">
        <el-input v-model="editForm.customerAge"></el-input>
        </el-form-item>
        <el-form-item label="性别" prop="customerSex">
        <el-radio-group v-model="editForm.customerSex">
        <el-radio value="男">男</el-radio>
        <el-radio value="女">女</el-radio>
        </el-radio-group>
        </el-form-item>
        <el-form-item label="电话" prop="customerPhone">
        <el-input v-model="editForm.customerPhone"></el-input>
        </el-form-item>
        <el-form-item label="地址" prop="customerAddress">
        <el-input v-model="editForm.customerAddress"></el-input>
        </el-form-item>
      </el-form>
      <template #footer><span  class="dialog-footer">
      <el-button @click="editDialogVisible = false">取 消</el-button>
      <el-button type="primary" @click="saveGuardianEdit()" :loading="saving">确 定</el-button>
    </span></template>
    </el-dialog>

    <el-alert v-if="mutationError" :title="mutationError" type="error" :closable="false" show-icon />
    <div v-loading="isLoading" element-loading-text="正在加载监护人信息…">
    <div v-if="loadError" class="collection-feedback" role="alert"><span>{{ loadError }}</span><el-button plain :loading="isLoading" @click="loadGuardians(true)">重新加载</el-button></div>
    <el-table v-if="!mobileLayout && !loadError" class="adaptive-record-table" :data="guardianList"
    style="width: 100%" border>
    <template #empty><div  class="care-empty-state"><strong>暂无监护人信息</strong><span>添加监护人或调整搜索条件后，信息会显示在这里。</span></div></template>

      <el-table-column type="index" width="100" align="center"></el-table-column>
      <el-table-column prop="customerName" label="姓名" width="170" align="center"></el-table-column>
      <el-table-column prop="customerAge" label="年龄" width="170" align="center"></el-table-column>
	  <el-table-column prop="customerSex" label="性别" width="170" align="center"></el-table-column>
	  <el-table-column prop="customerPhone" label="电话" width="170" align="center"></el-table-column>
	  <el-table-column prop="customerAddress" label="地址" width="170" align="center"></el-table-column>
      <el-table-column label="操作" align="center">
        <template #default="scope" >
          <el-button size="small" plain type="primary" :icon="Edit"
                     @click="openEditDialog(scope.$index, scope.row)">编辑
          </el-button>
          <el-button size="small" plain type="danger" :icon="Delete" @click="deleteGuardian(scope.row.customerId)">删除
          </el-button>
          
        </template>
      </el-table-column>
    </el-table>
    <record-cards v-if="mobileLayout && !loadError" :records="guardianList" :fields='[{"key":"customerAge","label":"年龄"},{"key":"customerSex","label":"性别"},{"key":"customerPhone","label":"电话"},{"key":"customerAddress","label":"地址"}]' label="监护人资料" title-key="customerName" key-field="customerId">
      <template #actions="scope"  ><el-button size="small" plain type="primary" :icon="Edit"
                     @click="openEditDialog(scope.$index, scope.row)">编辑
          </el-button>
<el-button size="small" plain type="danger" :icon="Delete" @click="deleteGuardian(scope.row.customerId)">删除
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
  </div>

</template>

<script setup lang="ts">
import { Delete, Edit, Search } from '@element-plus/icons-vue'
import { useCollectionLayout } from '@/composables/useCollectionLayout'
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import RecordCards from '@/components/RecordCards.vue'
import { api } from '../api/client'
import { downloadCsv } from '../utils/csv'
import { useGuardians, validAge, validPhone } from '../composables/useGuardians'
const mobileLayout = useCollectionLayout(900)
const state = useGuardians(api)
const { searchText, createDialogVisible, editDialogVisible, createForm, editForm, isLoading, saving, loadError, mutationError, total, pageSize, currentPage, guardianList, loadGuardians, openEditDialog, resetCreateForm, handleSizeChange, changePage } = state
const createGuardianFormRef = ref<FormInstance>()
const editGuardianFormRef = ref<FormInstance>()
const rules = {
 customerPhone: [{ validator: (_rule: unknown, value: unknown, done: (err?: Error) => void) => done(validPhone(value) ? undefined : new Error('请输入有效的 11 位手机号')), trigger: 'blur' }],
 customerAge: [{ validator: (_rule: unknown, value: unknown, done: (err?: Error) => void) => done(validAge(value) ? undefined : new Error('请输入 0–150 岁的整数年龄')), trigger: 'blur' }]
}
async function createGuardian() {
 if (!await createGuardianFormRef.value?.validate().catch(() => false)) return
 if (await state.save()) ElMessage.success('添加监护人成功')
}
async function saveGuardianEdit() {
 if (!await editGuardianFormRef.value?.validate().catch(() => false)) return
 if (await state.save(true)) ElMessage.success('编辑监护人信息成功')
}
async function deleteGuardian(id: number | string | undefined) {
 if (id === undefined) return
 try { await ElMessageBox.confirm('确认删除该监护人？', '删除监护人', { type: 'warning' }) } catch { return }
 if (await state.remove(id)) ElMessage.success('删除监护人成功')
}
function exportCurrentPage () {
  if (!guardianList.value.length) { ElMessage.warning('当前没有可导出的数据'); return }
  downloadCsv(`监护人-${new Date().toISOString().slice(0, 10)}.csv`, [{ key: 'customerName', label: '姓名' }, { key: 'customerAge', label: '年龄' }, { key: 'customerSex', label: '性别' }, { key: 'customerPhone', label: '电话' }, { key: 'customerAddress', label: '地址' }], guardianList.value)
}
onMounted(() => loadGuardians())
</script>
