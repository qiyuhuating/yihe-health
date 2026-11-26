import { onBeforeUnmount, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { accepted, type Envelope } from '../api/client'
import { apiErrorMessage } from '../api/httpContract'
import type { DeviceId, DeviceRecord, BindCodeListResponse, NewBindCodeResponse, DeviceState } from './deviceModels'

type DevicePost = <T extends object = Record<string, never>>(
  path: string, params?: Record<string, unknown>
) => Promise<{ data: Envelope & T }>

// The selected resident owns the dialog. Late responses from another selection never publish.
export function useDeviceBindings(state: DeviceState, post: DevicePost) {
  let disposed = false
  let bindCodeRequest = 0
  let bindDialogRevision = 0
  let creatingCode = false
  const deletingCodes = new Set<string>()

  function openBindCodeDialog(row: DeviceRecord) {
    const wasVisible = state.bindCodeDialogVisible
    const oldId = row.oldId ?? ''
    const residentChanged = String(state.bindOldId) !== String(oldId)
    bindDialogRevision++
    state.currentDevice = row
    state.bindOldId = oldId
    state.bindExpireDays = '1'
    state.newBindCode = null
    state.bindCodeList = []
    state.bindCodeDialogVisible = true
    if (!wasVisible || !residentChanged) void loadBindCodes(oldId)
  }
  async function loadBindCodes(oldId: DeviceId) {
    const revision = ++bindCodeRequest
    if (disposed) return
    if (!oldId) {
      state.bindCodeList = []
      return
    }
    try {
      const { data } = await post<BindCodeListResponse>('bindCodeList', { oldId })
      // The dialog may now belong to another resident.
      if (disposed || revision !== bindCodeRequest || !state.bindCodeDialogVisible || String(state.bindOldId) !== String(oldId)) return
      state.bindCodeList = Array.isArray(data.bindCodes) ? data.bindCodes : []
    } catch (error) {
      if (!disposed && revision === bindCodeRequest && state.bindCodeDialogVisible) {
        ElMessage({ type: 'error', message: apiErrorMessage(error, '请求未完成，请重试') })
      }
    }
  }
  async function createBindCode() {
    if (creatingCode) return
    const oldId = state.bindOldId
    if (!oldId) {
      ElMessage({ type: 'warning', message: '请先选择要绑定的老人' })
      return
    }
    if (!/^\d+$/.test(state.bindExpireDays)) {
      ElMessage({ type: 'warning', message: '有效天数只能填数字' })
      return
    }
    creatingCode = true
    const revision = bindDialogRevision
    try {
      const { data } = await post<NewBindCodeResponse>('createBindCode', { oldId, expireDays: state.bindExpireDays })
      accepted(data)
      if (disposed || revision !== bindDialogRevision || !state.bindCodeDialogVisible || String(state.bindOldId) !== String(oldId)) return
      if (typeof data.code !== 'string' || typeof data.expireTime !== 'string') throw new Error('invalid binding code response')
      state.newBindCode = { code: data.code, expireTime: data.expireTime }
      await loadBindCodes(oldId)
    } catch (error) {
      if (!disposed && revision === bindDialogRevision && state.bindCodeDialogVisible && String(state.bindOldId) === String(oldId)) {
        ElMessage({ type: 'error', message: apiErrorMessage(error, '生成失败') })
      }
    } finally { creatingCode = false }
  }
  async function deleteBindCode(row: { id: DeviceId }) {
    const key = String(row.id)
    if (deletingCodes.has(key)) return
    deletingCodes.add(key)
    const revision = bindDialogRevision
    const oldId = state.bindOldId
    const isCurrent = () => !disposed && revision === bindDialogRevision &&
      state.bindCodeDialogVisible && String(state.bindOldId) === String(oldId)
    try {
      const { data } = await post('deleteBindCode', { id: row.id })
      accepted(data)
      if (!isCurrent()) return
      ElMessage({ type: 'success', message: '已作废' })
      await loadBindCodes(oldId)
    } catch (error) {
      if (!isCurrent()) return
      ElMessage({ type: 'error', message: apiErrorMessage(error, '请求未完成，请重试') })
    } finally {
      deletingCodes.delete(key)
    }
  }

  watch(() => state.bindOldId, oldId => {
    if (!state.bindCodeDialogVisible) return
    bindDialogRevision++
    state.bindCodeList = []
    state.newBindCode = null
    void loadBindCodes(oldId)
  }, { flush: 'sync' })
  watch(() => state.bindCodeDialogVisible, visible => {
    if (!visible) {
      bindCodeRequest++
      bindDialogRevision++
    }
  })

  onBeforeUnmount(() => {
    disposed = true
    bindCodeRequest++
    bindDialogRevision++
  })
  return { openBindCodeDialog, createBindCode, deleteBindCode }
}
