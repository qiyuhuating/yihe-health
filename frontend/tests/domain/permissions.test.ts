import { describe, expect, it } from 'vitest'
import { isRestrictedAdmin, mayManageCareStaff, mayReadCareAudit } from '@/domain/permissions'
import type { SessionAccount } from '@/domain/session'

const account = (duty?: string): SessionAccount => ({ role: 'ADMIN', duty })

describe('explicit care duties', () => {
  it('allows full administration only to explicitly assigned supervisors', () => {
    expect(isRestrictedAdmin(account('supervisor'))).toBe(false)
    expect(isRestrictedAdmin(account('caregiver'))).toBe(true)
    expect(isRestrictedAdmin(account('auditor'))).toBe(true)
    expect(isRestrictedAdmin(account())).toBe(true)
    expect(isRestrictedAdmin({ role: 'FAMILY' })).toBe(false)
    expect(isRestrictedAdmin({ role: 'FAMILY', duty: 'caregiver' })).toBe(false)
    expect(isRestrictedAdmin(null)).toBe(false)
  })

  it('provides audit access without granting auditors staff management', () => {
    expect(mayReadCareAudit(account('auditor'))).toBe(true)
    expect(mayReadCareAudit(account('supervisor'))).toBe(true)
    expect(mayReadCareAudit(account('caregiver'))).toBe(false)
    expect(mayReadCareAudit(account())).toBe(false)
    expect(mayManageCareStaff(account('supervisor'))).toBe(true)
    expect(mayManageCareStaff(account('auditor'))).toBe(false)
  })
})
