export type DeviceId = number | string

export function isValidCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

export interface DeviceRecord {
  deviceId: DeviceId
  deviceNo: string
  oldId?: DeviceId | null
  alias?: string
  online?: number | string
  battery?: number | string
  model?: string
  oldPersonName?: string
  lastTime?: string
  lastLng?: number | string | null
  lastLat?: number | string | null
  kind?: string
  kindText?: string
  reason?: string
  recordId?: DeviceId
}
// API device rows contain scalar fields. Compare the full public record
// without allocating a serialized copy on every 5-second poll; unknown fields
// are still considered so new API properties cannot be silently ignored.
export function sameDeviceRows(left: readonly DeviceRecord[], right: readonly DeviceRecord[]): boolean {
  if (left.length !== right.length) return false
  for (let index = 0; index < left.length; index++) {
    const a = left[index]
    const b = right[index]
    const fields = Object.keys(a) as (keyof DeviceRecord)[]
    if (fields.length !== Object.keys(b).length) return false
    for (const field of fields) {
      if (!Object.hasOwn(b, field) || a[field] !== b[field]) return false
    }
  }
  return true
}

export interface ResidentRecord { oldId: DeviceId; name: string }
export interface DeviceBindCode { id: DeviceId; code: string; expireTime: string; used: number }
export interface DeviceAlertCounts { personCount: number; rateCount: number; preCount: number }
export interface DeviceAlertCountRecord extends DeviceAlertCounts { deviceId: DeviceId }
export interface AlertCountsResponse {
  items: DeviceAlertCountRecord[]
  total: number
  personTotal?: number
  rateTotal?: number
  preTotal?: number
}
export interface DeviceListResponse { devices: DeviceRecord[]; total?: number; onlineCount?: number }
export interface AlertDetailResponse {
  hasAlert?: number
  message?: string
  oldPersonName?: string
  reason?: string
  detail?: string
  value?: string | number | null
  threshold?: string
  occurTime?: string
  advice?: string
  recordId?: DeviceId
  deviceNo?: string
  alias?: string
  kindText?: string
}
export interface CreatedDeviceResponse { deviceNo: string; secret: string }
export interface BindCodeListResponse { bindCodes?: DeviceBindCode[] }
export interface NewBindCodeResponse { code: string; expireTime: string }
export interface DeviceMutationResponse { secret?: string }
export interface DeviceAlert {
  visible: boolean
  loading: boolean
  resolving: boolean
  hasAlert: number
  title: string
  kind: string
  deviceId: DeviceId | null
  deviceNo: string
  alias: string
  oldPersonName: string
  reason: string
  detail: string
  value: string | number
  threshold: string
  occurTime: string
  advice: string
  recordId: DeviceId | null
  message: string
}
export interface DeviceState {
  deviceList: DeviceRecord[]
  deviceTotal: number
  serverOnlineCount: number
  currentPage: number
  pageSize: number
  reloadPending: boolean
  deviceDetailVisible: boolean
  detailDevice: DeviceRecord | null
  deviceQuery: string
  deviceState: string
  deviceLoading: boolean
  deviceLoadError: string
  alertLoadError: string
  alertsReady: boolean
  creating: boolean
  oldPeople: ResidentRecord[]
  autoRefresh: boolean
  createDialogVisible: boolean
  bindCodeDialogVisible: boolean
  createdDevice: CreatedDeviceResponse | null
  currentDevice: DeviceRecord | null
  bindCodeList: DeviceBindCode[]
  newBindCode: NewBindCodeResponse | null
  bindOldId: DeviceId
  bindExpireDays: string
  createForm: { alias: string; model: string; oldId: DeviceId }
  alertCounts: Record<string, DeviceAlertCounts>
  alertTotals: { personTotal: number; rateTotal: number; preTotal: number; total: number }
  alertDialog: DeviceAlert
  workbench: { visible: boolean; loading: boolean; list: DeviceRecord[] }
}


export function createDeviceState(): DeviceState {
  return {
    deviceList: [], deviceTotal: 0, serverOnlineCount: 0,
    currentPage: 1, pageSize: 20, reloadPending: false,
    deviceDetailVisible: false, detailDevice: null,
    deviceQuery: '', deviceState: 'all', deviceLoading: false,
    deviceLoadError: '', alertLoadError: '', alertsReady: false,
    creating: false, oldPeople: [], autoRefresh: true,
    createDialogVisible: false, bindCodeDialogVisible: false,
    createdDevice: null, currentDevice: null, bindCodeList: [], newBindCode: null,
    bindOldId: '', bindExpireDays: '1',
    createForm: { alias: '', model: 'HarmonyOS-Wear', oldId: '' },
    alertCounts: {},
    alertTotals: { personTotal: 0, rateTotal: 0, preTotal: 0, total: 0 },
    alertDialog: emptyDeviceAlert(),
    workbench: { visible: false, loading: false, list: [] }
  }
}

export function emptyDeviceAlert(): DeviceAlert {
  return {
    visible: false, loading: false, resolving: false, hasAlert: 1,
    title: '异常原因', kind: 'person', deviceId: null, deviceNo: '',
    alias: '', oldPersonName: '', reason: '', detail: '', value: '',
    threshold: '', occurTime: '', advice: '', recordId: null, message: ''
  }
}
