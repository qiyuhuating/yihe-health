import { apiErrorMessage } from '../api/httpContract'
import { ref, reactive, getCurrentScope, onScopeDispose } from 'vue'
import { accepted, type Envelope, type PostClient } from '../api/client'
import { createLatestRead } from './latestRead'

// UI guardians map to the backend's customer fields.
export interface Guardian {
  customerId?: number | string
  customerName: string
  customerAge: number | string
  customerSex: string
  customerPhone: string
  customerAddress: string
}
export const emptyGuardian = (): Guardian => ({ customerName: '', customerAge: '', customerSex: '', customerPhone: '', customerAddress: '' })
export const validPhone = (value: unknown) => /^1\d{10}$/.test(typeof value === 'string' || typeof value === 'number' ? String(value) : '')
export const validAge = (value: unknown) => {
  if (typeof value !== 'number' && typeof value !== 'string') return false
  if (typeof value === 'string' && !/^\d+$/.test(value.trim())) return false
  const age = Number(value)
  return Number.isInteger(age) && age >= 0 && age <= 150
}

export function useGuardians(client: PostClient) {
  const searchText = ref(''), createDialogVisible = ref(false), editDialogVisible = ref(false)
  const createForm = reactive(emptyGuardian()), editForm = reactive(emptyGuardian())
  const isLoading = ref(false), saving = ref(false), loadError = ref(''), mutationError = ref('')
  const total = ref(0), pageSize = ref(10), currentPage = ref(1), guardianList = ref<Guardian[]>([])
  type Query = { page: number; pageSize: number; customerName: string }
  const reads = createLatestRead(
    (query: Query, signal: AbortSignal) => client.post<{ customers: Guardian[]; total: number }>('api/v1/guardians/list', query, { signal }),
    ({ data }) => {
      if (!Array.isArray(data.customers) || !Number.isInteger(data.total) || data.total < 0) throw new Error('invalid list')
      guardianList.value = data.customers; total.value = data.total; loadError.value = ''
    },
    error => {
      guardianList.value = []; total.value = 0; loadError.value = apiErrorMessage(error, '监护人信息暂时无法加载，请检查服务连接后重试。')
    },
    value => { isLoading.value = value }
  )
  if (getCurrentScope()) onScopeDispose(reads.dispose)
  function loadGuardians(keepPage = false) {
    if (keepPage !== true) currentPage.value = 1
    loadError.value = ''
    return reads.run({ page: currentPage.value, pageSize: pageSize.value, customerName: searchText.value })
  }
  async function save(edit = false) {
    if (saving.value) return false
    const form = edit ? editForm : createForm
    if (!validPhone(form.customerPhone) || !validAge(form.customerAge) || !form.customerName.trim()) { mutationError.value = '请填写姓名、有效手机号与 0–150 岁的整数年龄'; return false }
    saving.value = true; mutationError.value = ''
    try {
      const { data } = await client.post<Envelope>(edit ? 'api/v1/guardians/update' : 'api/v1/guardians/create', { ...form })
      accepted(data)
      if (edit) editDialogVisible.value = false
      else { createDialogVisible.value = false; Object.assign(createForm, emptyGuardian()) }
      await loadGuardians(true); return true
    } catch (error) { mutationError.value = apiErrorMessage(error, '未收到保存确认，请核对记录'); return false }
    finally { saving.value = false }
  }
  async function remove(id: number | string) {
    if (saving.value) return false
    saving.value = true; mutationError.value = ''
    try { const { data } = await client.post<Envelope>('api/v1/guardians/delete', { customerId: id }); accepted(data); await loadGuardians(true); return true }
    catch (error) { mutationError.value = apiErrorMessage(error, '未收到删除确认，请刷新记录'); return false }
    finally { saving.value = false }
  }
  function openEditDialog(_index: unknown, row: Guardian) { Object.assign(editForm, emptyGuardian(), row); editDialogVisible.value = true; mutationError.value = '' }
  function resetCreateForm() { Object.assign(createForm, emptyGuardian()); mutationError.value = '' }
  function handleSizeChange(value: number) { pageSize.value = value; currentPage.value = 1; return loadGuardians(true) }
  function changePage(value: number) { currentPage.value = value; return loadGuardians(true) }
  return { searchText, createDialogVisible, editDialogVisible, createForm, editForm, isLoading, saving, loadError, mutationError, total, pageSize, currentPage, guardianList, loadGuardians, save, remove, openEditDialog, resetCreateForm, handleSizeChange, changePage }
}
