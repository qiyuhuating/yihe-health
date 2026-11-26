import { api, type Envelope } from './client'

export interface InvitationRecord {
  id: number
  code: string
  oldPersonName?: string
  createUser?: string
  createTime?: string
  expireTime?: string
  status: string
  usedTime?: string
}

export interface InvitableResident {
  oldId: string | number
  name: string
}

export interface InvitationResult extends Envelope {
  oldPersonName?: string
  remainDays?: number
  codes?: string[]
  inviteCodes?: InvitationRecord[]
  oldPeople?: InvitableResident[]
  total?: number
}

// Keep the Go invitation routes and cookie policy in one place.
export const invitations = {
  checkCode: (body: { code: string }) => api.post<InvitationResult>('api/v1/inviteCode/checkCode', body),
  join: (body: Record<string, unknown>) => api.post<InvitationResult>('api/v1/inviteCode/join', body),
  list: (body: { page: number; pageSize: number; state: string }) => api.post<InvitationResult>('api/v1/inviteCode/list', body),
  oldPersonList: () => api.post<InvitationResult>('api/v1/inviteCode/oldPersonList', {}),
  create: (body: Record<string, unknown>) => api.post<InvitationResult>('api/v1/inviteCode/create', body),
  delete: (body: { id: number }) => api.post<InvitationResult>('api/v1/inviteCode/delete', body)
}
