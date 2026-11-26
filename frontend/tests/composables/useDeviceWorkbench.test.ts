import { afterEach, expect, it, vi } from 'vitest'

afterEach(() => vi.unstubAllGlobals())
import { flushPromises } from '@vue/test-utils'
import { createDeviceState } from '@/composables/deviceModels'
import { useDeviceWorkbench } from '@/composables/useDeviceWorkbench'

it('settles cancelled workbench workers when a network adapter ignores abort', async () => {
  const state = createDeviceState()
  state.deviceList = [{ deviceId: 1, deviceNo: 'watch-1' }]
  state.alertCounts = { '1': { personCount: 1, rateCount: 0, preCount: 0 } }
  const workbench = useDeviceWorkbench(state, () => new Promise(() => {}))
  const pending = workbench.openWorkbench()
  workbench.disposeWorkbench()
  await expect(pending).resolves.toBeUndefined()
  expect(state.workbench.list).toEqual([])
})

it('cancels pending detail work when the alert dialog closes, then allows reopening', async () => {
  const state = createDeviceState()
  state.deviceList = [{ deviceId: 1, deviceNo: 'watch-1' }]
  state.alertCounts = { '1': { personCount: 1, rateCount: 0, preCount: 0 } }
  let calls = 0
  const workbench = useDeviceWorkbench(state, () => {
    calls++
    if (calls === 1) return new Promise(() => {})
    return Promise.resolve({ hasAlert: 1, kindText: '人员' })
  })
  const first = workbench.openWorkbench()
  await Promise.resolve()
  expect(state.workbench.loading).toBe(true)
  workbench.closeWorkbench()
  await first
  expect(state.workbench.visible).toBe(false)
  expect(state.workbench.loading).toBe(false)
  expect(state.workbench.list).toEqual([])
  await workbench.openWorkbench()
  expect(state.workbench.visible).toBe(true)
  expect(state.workbench.loading).toBe(false)
  expect(state.workbench.list).toHaveLength(1)
  workbench.disposeWorkbench()
})

it('shows the first completed alert while another request is still waiting', async () => {
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    queueMicrotask(() => callback(0))
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
  const state = createDeviceState()
  state.deviceList = [{ deviceId: 1, deviceNo: 'watch-1' }, { deviceId: 2, deviceNo: 'watch-2' }]
  state.alertCounts = {
    '1': { personCount: 1, rateCount: 0, preCount: 0 },
    '2': { personCount: 1, rateCount: 0, preCount: 0 }
  }
  let finishSlow!: (value: { hasAlert: number; kindText: string }) => void
  const workbench = useDeviceWorkbench(state, (deviceId) => deviceId === 1
    ? Promise.resolve({ hasAlert: 1, kindText: '人员' })
    : new Promise(resolve => { finishSlow = resolve }))
  const pending = workbench.openWorkbench()
  await flushPromises()
  expect(state.workbench.loading).toBe(true)
  expect(state.workbench.list.map(row => row.deviceId)).toEqual([1])
  finishSlow({ hasAlert: 1, kindText: '人员' })
  await pending
  expect(state.workbench.loading).toBe(false)
  expect(state.workbench.list.map(row => row.deviceId)).toEqual([1, 2])
  workbench.disposeWorkbench()
})

it('groups simultaneous alert responses into one frame and discards stale frames on close', async () => {
  const state = createDeviceState()
  state.deviceList = Array.from({ length: 3 }, (_, index) => ({
    deviceId: index + 1, deviceNo: `watch-${index + 1}`
  }))
  state.alertCounts = Object.fromEntries(state.deviceList.map(row => [
    String(row.deviceId), { personCount: 1, rateCount: 0, preCount: 0 }
  ]))
  const callbacks: FrameRequestCallback[] = []
  const cancel = vi.fn()
  vi.stubGlobal('requestAnimationFrame', vi.fn((callback: FrameRequestCallback) => {
    callbacks.push(callback)
    return callbacks.length
  }))
  vi.stubGlobal('cancelAnimationFrame', cancel)
  let finishSlow!: (value: { hasAlert: number; kindText: string }) => void
  const workbench = useDeviceWorkbench(state, deviceId => Number(deviceId) === 3
    ? new Promise(resolve => { finishSlow = resolve })
    : Promise.resolve({ hasAlert: 1, kindText: '人员' }))
  const pending = workbench.openWorkbench()
  await flushPromises()
  expect(callbacks).toHaveLength(1)
  expect(state.workbench.list).toEqual([])
  callbacks[0](0)
  expect(state.workbench.list.map(row => row.deviceId)).toEqual([1, 2])
  finishSlow({ hasAlert: 1, kindText: '人员' })
  await pending
  expect(state.workbench.list.map(row => row.deviceId)).toEqual([1, 2, 3])
  workbench.disposeWorkbench()

  const stale = useDeviceWorkbench(state, () => Promise.resolve({ hasAlert: 1, kindText: '人员' }))
  const another = stale.openWorkbench()
  await flushPromises()
  // Pending progress was rendered for at most one frame, but closing
  // must never allow callbacks from the previous dialog to publish.
  stale.closeWorkbench()
  for (const callback of callbacks.slice(1)) callback(0)
  await another
  expect(state.workbench.visible).toBe(false)
  stale.disposeWorkbench()
})
