import { expect, it, vi } from 'vitest'
const mock = vi.hoisted(() => ({
  request: null as unknown,
  responseError: null as unknown,
  get: vi.fn(),
  post: vi.fn()
}))
vi.mock('axios', async (importOriginal) => ({ ...await importOriginal<typeof import('axios')>(), default: {
  defaults: {}, get: mock.get, post: mock.post,
  interceptors: { request: { use: (fn: unknown) => { mock.request = fn } }, response: { use: (_success: unknown, error: unknown) => { mock.responseError = error } } }
} }))
import { authState, endSession, installHttpSecurity, loadSession } from '@/auth'

it('adds CSRF to versioned mutations without leaking it to another origin', async () => {
  mock.get.mockResolvedValue({ data: { csrfToken: 'session-csrf', authenticated: false } })
  installHttpSecurity({ currentRoute: { value: { path: '/sign-in' } } })
  const request = { url: '/api/v1/security/factor', method: 'post', headers: {} as Record<string, string> }
  await (mock.request as (config: typeof request) => unknown)(request)
  expect(request.headers['X-CSRF-TOKEN']).toBe('session-csrf')
  const external = { url: 'https://other.example/api/v1/security/factor', method: 'post', headers: {} }
  await (mock.request as (config: typeof external) => unknown)(external)
  expect(external.headers).toEqual({})
})

it('refreshes a downgraded ADMIN duty on forbidden response without replaying the write', async () => {
  const router = { currentRoute: { value: { path: '/operations/guardians' } }, replace: vi.fn() }
  authState.account = { role: 'ADMIN', duty: 'supervisor' }
  mock.get.mockResolvedValue({
    data: { csrfToken: 'rotated', authenticated: true, account: { role: 'ADMIN', duty: 'caregiver' } }
  })
  installHttpSecurity(router)
  const denied = { response: { status: 403, data: { code: 'ACCESS_DENIED' } } }
  await expect((mock.responseError as (error: unknown) => Promise<unknown>)(denied)).rejects.toBe(denied)
  await vi.waitFor(() => expect(router.replace).toHaveBeenCalledWith('/operations/overview'))
  expect(authState.account?.duty).toBe('caregiver')
  expect(mock.get).toHaveBeenCalledWith('/api/v1/auth/session', expect.objectContaining({ skipSession: true }))
})


it('ignores a session response that completes after logout', async () => {
  let finishSession: (value: unknown) => void = () => {}
  mock.get.mockImplementationOnce(() => new Promise(resolve => { finishSession = resolve }))
  const staleRequest = loadSession(true)
  mock.post.mockResolvedValueOnce({ data: { returnCode: '1111' } })
  await endSession()
  finishSession({ data: {
    csrfToken: 'stale-csrf', authenticated: true,
    account: { id: 7, role: 'ADMIN', displayName: 'stale administrator' }
  } })
  await staleRequest
  expect(authState.account).toBeNull()
  expect(authState.csrfToken).toBe('')
})

it('does not apply a pending session response after a 401', async () => {
  let finishSession: (value: unknown) => void = () => {}
  mock.get.mockImplementationOnce(() => new Promise(resolve => { finishSession = resolve }))
  const staleRequest = loadSession(true)
  const router = { currentRoute: { value: { path: '/operations/overview' } }, replace: vi.fn() }
  installHttpSecurity(router)
  const denied = { response: { status: 401, data: { code: 'SESSION_EXPIRED' } } }
  await expect((mock.responseError as (error: unknown) => Promise<unknown>)(denied)).rejects.toBe(denied)
  finishSession({ data: {
    csrfToken: 'expired-csrf', authenticated: true,
    account: { id: 8, role: 'ADMIN', displayName: 'expired administrator' }
  } })
  await staleRequest
  expect(authState.account).toBeNull()
  expect(authState.csrfToken).toBe('')
  expect(router.replace).toHaveBeenCalledWith('/sign-in')
})

it('refreshes a rejected CSRF token without discarding the authenticated account', async () => {
  authState.account = { id: 9, role: 'ADMIN', duty: 'supervisor' }
  authState.csrfToken = 'expired-token'
  mock.get.mockResolvedValueOnce({
    data: {
      csrfToken: 'renewed-token',
      authenticated: true,
      account: { id: 9, role: 'ADMIN', duty: 'supervisor' }
    }
  })
  installHttpSecurity({ currentRoute: { value: { path: '/operations/overview' } } })
  const denied = { response: { status: 403, data: { code: 'CSRF_REJECTED' } } }
  await expect((mock.responseError as (error: unknown) => Promise<unknown>)(denied)).rejects.toBe(denied)
  await vi.waitFor(() => expect(authState.csrfToken).toBe('renewed-token'))
  expect(authState.account).toMatchObject({ id: 9, role: 'ADMIN', duty: 'supervisor' })
})

it('rejects a malformed session before adopting the identity or CSRF token', async () => {
  authState.account = null
  authState.csrfToken = ''
  mock.get.mockResolvedValueOnce({ data: {
    csrfToken: 123, authenticated: true,
    account: { role: 'ADMIN', id: 12 }
  } })
  await expect(loadSession(true)).rejects.toThrow('Invalid session response')
  expect(authState.account).toBeNull()
  expect(authState.csrfToken).toBe('')
})