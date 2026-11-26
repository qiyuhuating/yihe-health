import type { SessionAccount } from './session'

// An explicit supervisor assignment is required for the full administration UI.
// Every other ADMIN duty, including an unassigned account, stays in the care workspace.
export function isRestrictedAdmin(account: SessionAccount | null): boolean {
  return account?.role === 'ADMIN' && account.duty !== 'supervisor'
}

export function mayReadCareAudit(account: SessionAccount | null): boolean {
  return account?.role === 'ADMIN' && (account.duty === 'supervisor' || account.duty === 'auditor')
}

export function mayManageCareStaff(account: SessionAccount | null): boolean {
  return account?.role === 'ADMIN' && account.duty === 'supervisor'
}
