import { onBeforeUnmount, onMounted, reactive, toRefs, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { accepted, api, type Envelope } from '../api/client'
import { apiErrorMessage, readErrorMessage } from '../api/httpContract'
import { copyText } from '../utils/clipboard'
import { createDeviceState, emptyDeviceAlert, isValidCount, sameDeviceRows } from './deviceModels'
import { useDeviceBindings } from './useDeviceBindings'
import { useDeviceWorkbench } from './useDeviceWorkbench'
export type { DeviceRecord } from './deviceModels'
import type {
  DeviceId, DeviceRecord, ResidentRecord, DeviceAlertCounts, AlertCountsResponse,
  DeviceListResponse, AlertDetailResponse, CreatedDeviceResponse,
  DeviceMutationResponse, DeviceState
} from './deviceModels'

const post = <T extends object = Record<string, never>>(path: string, params: Record<string, unknown> = {}) => api.post<Envelope & T>('/api/v1/device/' + path, params)

export function useDevices() {
  const router = useRouter()
  const state = reactive<DeviceState>(createDeviceState())
  const { openBindCodeDialog, createBindCode, deleteBindCode } = useDeviceBindings(state, post)
  const { openWorkbench, closeWorkbench, disposeWorkbench } = useDeviceWorkbench(state, async (deviceId, kind, signal) => {
    const { data } = await api.post<Envelope & AlertDetailResponse>(
      '/api/v1/device/alertDetail',
      { deviceId, kind },
      { signal }
    )
    return data
  })
  let filterTimer: ReturnType<typeof setTimeout> | undefined
  let refreshTimer: ReturnType<typeof setTimeout> | undefined
  let disposed = false
  let displayedQuery = ''
  let consecutivePollFailures = 0
  // Only the newest read may publish state, even if an older request ignores cancellation.
  let alertRequest = 0
  let alertCountsRequest = 0
  const pendingResolutions = reactive(new Set<string>())
  const alertKey = (row: DeviceRecord) => `${row.deviceId}:${row.kind ?? ''}:${row.recordId ?? ''}`
  const isResolvingAlert = (row: DeviceRecord) => pendingResolutions.has(alertKey(row))

  async function loadAlertCounts() {
    const revision = ++alertCountsRequest
    if (disposed) return
    try {
      const { data } = await post<AlertCountsResponse>('alertCounts')
      if (disposed || revision !== alertCountsRequest) return
      if (!Array.isArray(data.items) || !isValidCount(data.total) ||
        !data.items.every(item => isValidCount(item.personCount) && isValidCount(item.rateCount) && isValidCount(item.preCount)) ||
        ![data.personTotal, data.rateTotal, data.preTotal].every(count => count === undefined || isValidCount(count))) {
        throw new Error('invalid alert counters')
      }
      const counts = Object.fromEntries(data.items.map(item => [item.deviceId, item]))
      const previousCounts = state.alertCounts
      const ids = Object.keys(counts)
      if (ids.length !== Object.keys(previousCounts).length || ids.some(id => {
        const before = previousCounts[id]
        const after = counts[id]
        return !before || before.personCount !== after.personCount ||
          before.rateCount !== after.rateCount || before.preCount !== after.preCount
      })) state.alertCounts = counts
      const totals = {
        personTotal: data.personTotal ?? 0, rateTotal: data.rateTotal ?? 0,
        preTotal: data.preTotal ?? 0, total: data.total
      }
      if (state.alertTotals.personTotal !== totals.personTotal ||
          state.alertTotals.rateTotal !== totals.rateTotal ||
          state.alertTotals.preTotal !== totals.preTotal ||
          state.alertTotals.total !== totals.total) state.alertTotals = totals
      state.alertsReady = true
      state.alertLoadError = ''
    } catch (error) {
      if (disposed || revision !== alertCountsRequest) return
      state.alertsReady = false
      state.alertLoadError = readErrorMessage(error, '异常统计暂时无法更新，请重新加载。')
    }
  }

  let deviceReadTask: Promise<void> | undefined
  function loadDevices(): Promise<void> {
    if (disposed) return Promise.resolve()
    if (deviceReadTask) {
      state.reloadPending = true
      return deviceReadTask
    }
    deviceReadTask = (async () => {
      do {
        state.reloadPending = false
        await readDeviceSnapshot()
      } while (state.reloadPending && !disposed)
    })().finally(() => { deviceReadTask = undefined })
    return deviceReadTask
  }

  async function readDeviceSnapshot(): Promise<void> {
    const query = {
      page: state.currentPage, pageSize: state.pageSize,
      query: state.deviceQuery, state: state.deviceState
    }
    const isCurrentQuery = () =>
      query.page === state.currentPage && query.pageSize === state.pageSize &&
      query.query === state.deviceQuery && query.state === state.deviceState
    const queryKey = JSON.stringify(query)
    // Keep the table responsive on unchanged background reads, but signal a changed filter.
    state.deviceLoading = state.deviceList.length === 0 || displayedQuery !== queryKey
    try {
      await Promise.all([
        post<DeviceListResponse>('list', query).then(({ data }) => {
          if (disposed) return
          if (!isCurrentQuery()) {
            state.reloadPending = true
            return
          }
          if (!Array.isArray(data.devices) ||
             (data.total !== undefined && !isValidCount(data.total)) ||
             (data.onlineCount !== undefined && !isValidCount(data.onlineCount))) {
             throw new Error('invalid device list')
           }
          if (data.total !== undefined) {
            const lastPage = Math.max(1, Math.ceil(data.total / state.pageSize))
            if (state.currentPage > lastPage) {
              state.currentPage = lastPage
              state.reloadPending = true
              return
            }
          }
          if (!sameDeviceRows(state.deviceList, data.devices)) state.deviceList = data.devices
          displayedQuery = queryKey
          state.deviceLoadError = ''
          state.deviceTotal = data.total ?? data.devices.length
          state.serverOnlineCount = data.onlineCount ??
            data.devices.filter(item => item.online === 1 || item.online === '1').length
        }).catch(error => {
          if (disposed) return
          if (!isCurrentQuery()) {
            state.reloadPending = true
            return
          }
          state.deviceLoadError = readErrorMessage(error, '设备列表暂时无法更新，请重新加载。')
        }),
        loadAlertCounts()
      ])
    } finally {
      if (!disposed) {
        consecutivePollFailures = state.deviceLoadError || state.alertLoadError
          ? Math.min(consecutivePollFailures + 1, 3) : 0
        state.deviceLoading = false
      }
    }
  }
  function scheduleFilter() {
    clearTimeout(filterTimer)
    filterTimer = setTimeout(() => {
      state.currentPage = 1
      void loadDevices()
    }, 250)
  }
  function changeDevicePage(page: number) {
    state.currentPage = page
    void loadDevices()
  }

  function handleDeviceCommand(command: string, row: DeviceRecord) {
    if (command === 'details') {
      state.detailDevice = row
      state.deviceDetailVisible = true
    } else if (command === 'reset') void resetSecret(row)
    else if (command === 'unbind') void unbind(row)
    else if (command === 'delete') void deleteDevice(row)
  }
  function alertOf(deviceId: DeviceId): DeviceAlertCounts {
    return state.alertCounts[deviceId] || { personCount: 0, rateCount: 0, preCount: 0 }
  }
  async function showAlert(row: DeviceRecord, kind: string) {
    const revision = ++alertRequest
    state.alertDialog = {
      ...emptyDeviceAlert(), visible: true, loading: true,
      title: kind === 'rate' ? '心率不合格 · 原因与处理' :
        kind === 'pre' ? '血压不合格 · 原因与处理' : '人员不合格 · 原因与处理',
      kind, deviceId: row.deviceId, deviceNo: row.deviceNo, alias: row.alias || ''
    }
    try {
      const { data } = await post<AlertDetailResponse>('alertDetail', { deviceId: row.deviceId, kind })
      if (revision !== alertRequest || disposed || !state.alertDialog.visible) return
      Object.assign(state.alertDialog, {
        loading: false, hasAlert: data.hasAlert === undefined ? 1 : data.hasAlert,
        message: data.message || '', oldPersonName: data.oldPersonName || '',
        reason: data.reason || '', detail: data.detail || '',
        value: data.value == null ? '' : data.value, threshold: data.threshold || '',
        occurTime: data.occurTime || '', advice: data.advice || '',
        recordId: data.recordId === undefined ? null : data.recordId
      })
    } catch (error) {
      if (revision === alertRequest && !disposed && state.alertDialog.visible) {
        state.alertDialog.loading = false
        state.alertDialog.hasAlert = -1
        state.alertDialog.message = readErrorMessage(error, '读取异常详情失败')
      }
    }
  }
  function retryAlertDetail() {
    if (!state.alertDialog.visible || state.alertDialog.loading || state.alertDialog.resolving ||
        state.alertDialog.deviceId == null) return
    const { deviceId, deviceNo, alias, kind } = state.alertDialog
    void showAlert({ deviceId, deviceNo, alias }, kind)
  }
  async function resolveAlert() {
    if (state.alertDialog.resolving) return
    state.alertDialog.resolving = true
    try {
      const { data } = await post('resolveAlert', {
        deviceId: state.alertDialog.deviceId, kind: state.alertDialog.kind,
        recordId: state.alertDialog.recordId == null ? '' : state.alertDialog.recordId
      })
      accepted(data)
      ElMessage({ type: 'success', message: '已处理，红点已消除' })
      state.alertDialog.visible = false
      await loadDevices()
    } catch (error) {
      ElMessage({ type: 'error', message: apiErrorMessage(error, '处理失败') })
    } finally { state.alertDialog.resolving = false }
  }
  async function resolveAlertRow(row: DeviceRecord) {
    const key = alertKey(row)
    if (pendingResolutions.has(key)) return
    pendingResolutions.add(key)
    try {
      const { data } = await post('resolveAlert', {
        deviceId: row.deviceId, kind: row.kind, recordId: row.recordId ?? ''
      })
      accepted(data)
      ElMessage({ type: 'success', message: '已处理，红点已消除' })
      // A successful write may update the visible result immediately; reads reconcile in parallel.
      const wasOpen = state.workbench.visible
      if (wasOpen) {
        state.workbench.list = state.workbench.list.filter(item =>
          !(String(item.deviceId) === String(row.deviceId) && item.kind === row.kind &&
            String(item.recordId ?? '') === String(row.recordId ?? '')))
      }
      void loadDevices().then(() => {
        if (wasOpen && state.workbench.visible && !disposed) void openWorkbench()
      })
    } catch (error) {
      if (!disposed) ElMessage({ type: 'error', message: apiErrorMessage(error, '处理失败') })
    } finally {
      pendingResolutions.delete(key)
    }
  }
  function gotoPage(kind: string) {
    void router.push(kind === 'person' ? '/operations/monitoring-profiles' : '/operations/vitals')
    state.alertDialog.visible = false
  }

  async function loadOldPeople() {
    try {
      const { data } = await api.post<{ oldPeople: ResidentRecord[] }>('/api/v1/inviteCode/oldPersonList', {})
      if (!disposed) state.oldPeople = Array.isArray(data.oldPeople) ? data.oldPeople : []
    } catch (error) {
      if (!disposed) ElMessage({ type: 'error', message: readErrorMessage(error, '读取老人列表失败') })
    }
  }
  function stopTimer() {
    clearTimeout(refreshTimer)
    refreshTimer = undefined
  }
  function startTimer() {
    stopTimer()
    if (!state.autoRefresh || document.hidden || disposed) return
    const delay = 5000 * (2 ** consecutivePollFailures)
    refreshTimer = setTimeout(async () => {
      refreshTimer = undefined
      try { await loadDevices() } finally { startTimer() }
    }, delay)
  }
  async function handleVisibilityChange() {
    stopTimer()
    if (!document.hidden && state.autoRefresh) {
      await loadDevices()
      startTimer()
    }
  }
  function handleOnline() {
    if (disposed || !state.autoRefresh || document.hidden) return
    consecutivePollFailures = 0
    stopTimer()
    void loadDevices().finally(startTimer)
  }
  function closeCreateDialog() {
    state.createDialogVisible = false
    state.createdDevice = null
    state.createForm = { alias: '', model: 'HarmonyOS-Wear', oldId: '' }
  }
  async function createDevice() {
    if (state.creating) return
    state.creating = true
    try {
      const { data } = await post<CreatedDeviceResponse>('create', {
        alias: state.createForm.alias || '新手表',
        model: state.createForm.model || 'HarmonyOS-Wear',
        oldId: state.createForm.oldId === '' ? '' : state.createForm.oldId
      })
      accepted(data)
      if (typeof data.deviceNo !== 'string' || typeof data.secret !== 'string') throw new Error('invalid device registration response')
      if (disposed) return
      state.createdDevice = { deviceNo: data.deviceNo, secret: data.secret }
      ElMessage({ type: 'success', message: '设备登记成功' })
      await loadDevices()
    } catch (error) {
      if (!disposed) ElMessage({ type: 'error', message: apiErrorMessage(error, '登记失败') })
    } finally { state.creating = false }
  }
  async function destructiveOperation(row: DeviceRecord, prompt: string, path: string, success: string, onSuccess?: (data: Envelope & DeviceMutationResponse) => void | Promise<unknown>) {
    try {
      await ElMessageBox.confirm(prompt, '提示', {
        confirmButtonText: '确定', cancelButtonText: '取消', type: 'warning'
      })
    } catch { return }
    try {
      const { data } = await post<DeviceMutationResponse>(path, { deviceId: row.deviceId })
      accepted(data)
      if (disposed) return
      if (onSuccess) await onSuccess(data)
      else ElMessage({ type: 'success', message: success })
      await loadDevices()
    } catch (error) {
      if (!disposed) ElMessage({ type: 'error', message: apiErrorMessage(error, '设备操作未完成，请重试') })
    }
  }
  function resetSecret(row: DeviceRecord) {
    return destructiveOperation(
      row, '重置后手表端需要用新密钥重新登录，是否继续？',
      'resetSecret', '密钥已重置',
      data => ElMessageBox.alert('新密钥：' + String(data.secret ?? ''), '密钥已重置', { confirmButtonText: '知道了' })
    )
  }
  function unbind(row: DeviceRecord) {
    return destructiveOperation(
      row, '解绑后该手表将不再上报到这位老人，是否继续？', 'unbind', '已解绑'
    )
  }
  function deleteDevice(row: DeviceRecord) {
    return destructiveOperation(
      row, '删除后需要重新登记设备，是否继续？', 'delete', '已删除'
    )
  }
  async function copy(value: string) {
    try {
      await copyText(value)
      ElMessage({ type: 'success', message: '已复制' })
    } catch {
      ElMessage({ type: 'warning', message: '复制失败，请手动记录。' })
    }
  }

  watch(() => state.deviceQuery, scheduleFilter)
  watch(() => state.deviceState, scheduleFilter)
  watch(() => state.autoRefresh, enabled => enabled ? startTimer() : stopTimer())
  const onVisibilityChange = () => { void handleVisibilityChange() }
  onMounted(() => {
    void loadDevices().finally(startTimer)
    void loadOldPeople()
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('online', handleOnline)
  })
  onBeforeUnmount(() => {
    disposed = true
    alertRequest++
    alertCountsRequest++
    disposeWorkbench()
    clearTimeout(filterTimer)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('online', handleOnline)
    stopTimer()
  })
  return {
    ...toRefs(state),
    loadDevices, changeDevicePage, handleDeviceCommand,
    alertOf, showAlert, retryAlertDetail, resolveAlert, resolveAlertRow, isResolvingAlert, openWorkbench, closeWorkbench, gotoPage,
    closeCreateDialog, createDevice, openBindCodeDialog,
    createBindCode, deleteBindCode, resetSecret, unbind, deleteDevice, copy
  }
}
