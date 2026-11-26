// Shared account identity and authorization data. No dependency on session transport.
export interface SessionAccount {
  role: 'ADMIN' | 'FAMILY'
  id?: number | string
  duty?: string
  displayName?: string
  oldId?: number | string
  phone?: string
}
