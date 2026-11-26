export interface VitalAlertState {
  visible: boolean
  loading: boolean
  resolving: boolean
  hasAlert: number
  title: string
  recordId: number | string | null
  oldPersonName: string
  hr: number | string
  bp: number | string
  rateBad: number
  preBad: number
  reason: string
  detail: string
  occurTime: string
  advice: string
  message: string
}

export interface VitalAlertDetail {
  hasAlert?: number
  message?: string
  recordId?: number | string | null
  oldPersonName?: string
  hr?: number | string | null
  bp?: number | string | null
  rateBad?: number
  preBad?: number
  reason?: string
  detail?: string
  occurTime?: string
  advice?: string
}
