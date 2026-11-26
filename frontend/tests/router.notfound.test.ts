import { expect, it, vi } from 'vitest'

const session = vi.hoisted(() => ({ account: { role: 'FAMILY', duty: '' } }))
vi.mock('@/auth', () => ({
  authState: { setupRequired: false, get account() { return session.account } },
  loadSession: vi.fn(async () => session.account)
}))

import router from '@/router'

it('keeps unknown paths on the 404 page for authenticated family and restricted admin accounts', async () => {
  await router.push('/missing-family-page')
  expect(router.currentRoute.value.name).toBe('not-found')

  session.account.role = 'ADMIN'
  session.account.duty = 'auditor'
  await router.push('/missing-admin-page')
  expect(router.currentRoute.value.name).toBe('not-found')
})
