import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import DatabaseNotice from '@/components/DatabaseNotice.vue'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('axios', async (importOriginal) => ({ ...await importOriginal<typeof import('axios')>(), default: { get } }))
import { authState, monitorDatabase } from '@/auth'

beforeEach(() => {
  get.mockReset()
  authState.databaseReady = false
  authState.configError = ''
})
afterEach(() => { vi.useRealTimers() })

it('distinguishes configuration errors from reconnecting and preserves the ready slot', async () => {
  authState.configError = 'DATA_ENCRYPTION_KEY <invalid>'
  const view = mount(DatabaseNotice, { slots: { default: '<button>Business action</button>' } })
  expect(view.get('[role=alert]').text()).toContain(authState.configError)
  expect(view.text()).not.toContain('正在自动连接')
  expect(view.find('invalid').exists()).toBe(false)
  expect(view.find('button').exists()).toBe(false)

  authState.configError = ''
  await nextTick()
  expect(view.get('[role=status]').text()).toContain('正在自动连接')
  authState.databaseReady = true
  await nextTick()
  expect(view.get('button').text()).toBe('Business action')
  view.unmount()
})

it('reads configuration diagnostics and clears them when a later status omits the field', async () => {
  vi.useFakeTimers()
  get.mockResolvedValueOnce({ data: { databaseReady: false, configError: 'Invalid session key' } })
    .mockResolvedValueOnce({ data: { databaseReady: false } })
  const stop = monitorDatabase({ currentRoute: { value: { path: '/sign-in' } } })
  try {
    await vi.advanceTimersByTimeAsync(0)
    expect(authState.configError).toBe('Invalid session key')
    await vi.advanceTimersByTimeAsync(3000)
    expect(authState.configError).toBe('')
    expect(get).toHaveBeenCalledTimes(2)
  } finally { stop() }
  await vi.advanceTimersByTimeAsync(3000)
  expect(get).toHaveBeenCalledTimes(2)
})
