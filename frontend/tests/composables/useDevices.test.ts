import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { expect, it, vi } from 'vitest'
import axios from 'axios'
import { useDevices } from '@/composables/useDevices'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

it('keeps binding codes scoped to the currently selected resident', async () => {
  const replies = new Map<string, (value: unknown) => void>()
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/bindCodeList') {
      return new Promise(resolve => { replies.set(String((body as { oldId?: string | number }).oldId), resolve) })
    }
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({
    setup() { devices = useDevices(); return () => null }
  }))
  try {
    await flushPromises()
    devices.openBindCodeDialog({ deviceId: 1, deviceNo: 'watch-1', oldId: 1 })
    await flushPromises()
    devices.bindOldId.value = 2
    await flushPromises()
    replies.get('2')?.({ data: { bindCodes: [{ id: 2, code: 'new', expireTime: 'tomorrow', used: 0 }] } })
    await flushPromises()
    replies.get('1')?.({ data: { bindCodes: [{ id: 1, code: 'old', expireTime: 'yesterday', used: 0 }] } })
    await flushPromises()
    expect(devices.bindCodeList.value).toEqual([{ id: 2, code: 'new', expireTime: 'tomorrow', used: 0 }])
    devices.bindCodeDialogVisible.value = false
    await flushPromises()
    expect(devices.bindCodeList.value).toEqual([{ id: 2, code: 'new', expireTime: 'tomorrow', used: 0 }])
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('does not attach a created binding code to a newly selected resident', async () => {
  const replies = new Map<string, (value: unknown) => void>()
  let finishCreate!: (value: unknown) => void
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/bindCodeList') {
      return new Promise(resolve => { replies.set(String((body as { oldId?: string | number }).oldId), resolve) })
    }
    if (url === '/api/v1/device/createBindCode') return new Promise(resolve => { finishCreate = resolve })
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({
    setup() { devices = useDevices(); return () => null }
  }))
  try {
    await flushPromises()
    devices.openBindCodeDialog({ deviceId: 1, deviceNo: 'watch-1', oldId: 1 })
    await flushPromises()
    const creating = devices.createBindCode()
    await flushPromises()
    devices.bindOldId.value = 2
    await flushPromises()
    replies.get('2')?.({ data: { bindCodes: [{ id: 2, code: 'new', expireTime: 'tomorrow', used: 0 }] } })
    await flushPromises()
    finishCreate({ data: { returnCode: '1111', code: 'old', expireTime: 'today' } })
    await creating
    expect(devices.newBindCode.value).toBeNull()
    expect(devices.bindCodeList.value).toEqual([{ id: 2, code: 'new', expireTime: 'tomorrow', used: 0 }])
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('does not publish device rows from a query superseded during an active read', async () => {
  const listReplies: ((value: unknown) => void)[] = []
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') return new Promise(resolve => { listReplies.push(resolve) })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({
    setup() { devices = useDevices(); return () => null }
  }))
  try {
    await flushPromises()
    expect(listReplies).toHaveLength(1)
    devices.changeDevicePage(2)
    listReplies[0]({ data: { devices: [{ deviceId: 1, deviceNo: 'stale' }], total: 1 } })
    await flushPromises()
    expect(devices.deviceList.value).toEqual([])
    expect(listReplies).toHaveLength(2)
    listReplies[1]({ data: { devices: [{ deviceId: 2, deviceNo: 'current' }], total: 21 } })
    await flushPromises()
    expect(devices.deviceList.value).toEqual([{ deviceId: 2, deviceNo: 'current' }])
    expect(devices.currentPage.value).toBe(2)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('limits workbench alert reads to six in flight and preserves device order', async () => {
  const devices = Array.from({ length: 12 }, (_, index) => ({ deviceId: index + 1, deviceNo: `watch-${index + 1}` }))
  const finish: Array<() => void> = []
  let active = 0
  let peak = 0
  vi.spyOn(axios, 'post').mockImplementation((url) => {
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices, total: devices.length } })
    if (url === '/api/v1/device/alertCounts') {
      return Promise.resolve({ data: {
        items: devices.map(device => ({ deviceId: device.deviceId, personCount: 1, rateCount: 0, preCount: 0 })),
        total: devices.length
      } })
    }
    if (url === '/api/v1/device/alertDetail') {
      active += 1
      peak = Math.max(peak, active)
      return new Promise(resolve => finish.push(() => {
        active -= 1
        resolve({ data: { hasAlert: 1 } })
      }))
    }
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let state!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { state = useDevices(); return () => null } }))
  try {
    await flushPromises()
    const pending = state.openWorkbench()
    await flushPromises()
    expect(finish).toHaveLength(6)
    expect(peak).toBe(6)
    for (let index = 0; index < devices.length; index++) {
      finish[index]()
      await flushPromises()
    }
    await pending
    expect(peak).toBe(6)
    expect(state.workbench.value.list.map(row => row.deviceId)).toEqual(devices.map(row => row.deviceId))
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('keeps healthy workbench alerts visible when another detail request fails', async () => {
  const devices = [
    { deviceId: 1, deviceNo: 'watch-1' },
    { deviceId: 2, deviceNo: 'watch-2' },
    { deviceId: 3, deviceNo: 'watch-3' }
  ]
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices, total: 3 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: {
      items: devices.map(device => ({ deviceId: device.deviceId, personCount: 1, rateCount: 0, preCount: 0 })),
      total: 3
    } })
    if (url === '/api/v1/device/alertDetail') {
      const id = (body as { deviceId: number }).deviceId
      return id === 2 ? Promise.reject(new Error('temporary failure')) :
        Promise.resolve({ data: { hasAlert: 1 } })
    }
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let state!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { state = useDevices(); return () => null } }))
  try {
    await flushPromises()
    await state.openWorkbench()
    expect(state.workbench.value.list.map(row => row.deviceId)).toEqual([1, 3])
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('clears previous workbench results while refreshing alert details', async () => {
  const pendingDetails: Array<(value: unknown) => void> = []
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') {
      return Promise.resolve({ data: { devices: [{ deviceId: 1, deviceNo: 'watch-1' }], total: 1 } })
    }
    if (url === '/api/v1/device/alertCounts') {
      return Promise.resolve({ data: { items: [{ deviceId: 1, personCount: 1, rateCount: 0, preCount: 0 }], total: 1 } })
    }
    if (url === '/api/v1/device/alertDetail') {
      return new Promise(resolve => pendingDetails.push(resolve))
    }
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let state!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { state = useDevices(); return () => null } }))
  try {
    await flushPromises()
    const first = state.openWorkbench()
    await flushPromises()
    pendingDetails[0]({ data: { hasAlert: 1 } })
    await first
    expect(state.workbench.value.list).toHaveLength(1)

    const refresh = state.openWorkbench()
    expect(state.workbench.value.list).toEqual([])
    await flushPromises()
    pendingDetails[1]({ data: { hasAlert: 1 } })
    await refresh
    expect(state.workbench.value.list).toHaveLength(1)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('reloads page one after deleting the final device on a later page', async () => {
  const queries: number[] = []
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/list') {
      const page = (body as { page: number }).page
      queries.push(page)
      return Promise.resolve({ data: { devices: page === 1 ? [{ deviceId: 1, deviceNo: 'remaining' }] : [], total: 1 } })
    }
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let state!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { state = useDevices(); return () => null } }))
  try {
    await flushPromises()
    state.changeDevicePage(2)
    await flushPromises()
    expect(queries).toEqual([1, 2, 1])
    expect(state.currentPage.value).toBe(1)
    expect(state.deviceList.value).toEqual([{ deviceId: 1, deviceNo: 'remaining' }])
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('ignores a binding-code revocation response after switching residents', async () => {
  const replies = new Map<string, (value: unknown) => void>()
  let finishDelete!: (value: unknown) => void
  const listRequests: string[] = []
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/bindCodeList') {
      const oldId = String((body as { oldId: number }).oldId)
      listRequests.push(oldId)
      return new Promise(resolve => { replies.set(oldId, resolve) })
    }
    if (url === '/api/v1/device/deleteBindCode') {
      return new Promise(resolve => { finishDelete = resolve })
    }
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    devices.openBindCodeDialog({ deviceId: 1, deviceNo: 'watch-1', oldId: 1 })
    await flushPromises()
    replies.get('1')?.({ data: { bindCodes: [{ id: 10, code: 'old' }] } })
    await flushPromises()
    const removing = devices.deleteBindCode({ id: 10 })
    await flushPromises()
    devices.bindOldId.value = 2
    await flushPromises()
    replies.get('2')?.({ data: { bindCodes: [{ id: 20, code: 'current' }] } })
    await flushPromises()
    finishDelete({ data: { returnCode: '1111' } })
    await removing
    expect(devices.bindCodeList.value).toEqual([{ id: 20, code: 'current' }])
    expect(listRequests).toEqual(['1', '2'])
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('keeps the device table interactive during same-query refresh but shows loading for changed pages', async () => {
  const requests: Array<(value: unknown) => void> = []
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') return new Promise(resolve => { requests.push(resolve) })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    expect(devices.deviceLoading.value).toBe(true)
    requests[0]({ data: { devices: [{ deviceId: 1, deviceNo: 'watch-1' }], total: 21 } })
    await flushPromises()
    expect(devices.deviceLoading.value).toBe(false)

    const refresh = devices.loadDevices()
    expect(devices.deviceLoading.value).toBe(false)
    expect(devices.deviceList.value).toHaveLength(1)
    requests[1]({ data: { devices: [{ deviceId: 1, deviceNo: 'watch-1' }], total: 21 } })
    await refresh

    devices.changeDevicePage(2)
    expect(devices.deviceLoading.value).toBe(true)
    requests[2]({ data: { devices: [{ deviceId: 2, deviceNo: 'watch-2' }], total: 21 } })
    await flushPromises()
    expect(devices.deviceLoading.value).toBe(false)
    expect(devices.deviceList.value[0]?.deviceId).toBe(2)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('preserves reactive device and alert-count identities for unchanged polling results', async () => {
  let online = 1
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') return Promise.resolve({
      data: { devices: [{ deviceId: 1, deviceNo: 'watch-1', online }], total: 1, onlineCount: online }
    })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({
      data: { items: [{ deviceId: 1, personCount: 0, rateCount: 0, preCount: 0 }], total: 0 }
    })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    const firstRows = devices.deviceList.value
    const firstCounts = devices.alertCounts.value
    await devices.loadDevices()
    expect(devices.deviceList.value).toBe(firstRows)
    expect(devices.alertCounts.value).toBe(firstCounts)
    online = 0
    await devices.loadDevices()
    expect(devices.deviceList.value).not.toBe(firstRows)
    expect(devices.deviceList.value[0]?.online).toBe(0)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('backs off failed device polling and refreshes on network recovery', async () => {
  vi.useFakeTimers()
  let fail = true
  let listCalls = 0
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') {
      listCalls++
      return fail
        ? Promise.reject({ code: 'ERR_NETWORK' })
        : Promise.resolve({ data: { devices: [{ deviceId: 1, deviceNo: 'watch-1' }], total: 1 } })
    }
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await vi.advanceTimersByTimeAsync(0)
    expect(listCalls).toBe(1)
    expect(devices.deviceLoadError.value).toContain('网络')
    await vi.advanceTimersByTimeAsync(9999)
    expect(listCalls).toBe(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(listCalls).toBe(2)
    fail = false
    window.dispatchEvent(new Event('online'))
    await vi.advanceTimersByTimeAsync(0)
    expect(listCalls).toBe(3)
    expect(devices.deviceLoadError.value).toBe('')
    expect(devices.deviceList.value).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(4999)
    expect(listCalls).toBe(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(listCalls).toBe(4)
  } finally {
    wrapper.unmount()
    vi.useRealTimers()
    vi.restoreAllMocks()
  }
})

it('submits one binding-code revocation when the user clicks twice', async () => {
  let finishDelete!: (value: unknown) => void
  let deletes = 0
  vi.spyOn(axios, 'post').mockImplementation((url) => {
    if (url === '/api/v1/device/deleteBindCode') {
      deletes++
      return new Promise(resolve => { finishDelete = resolve })
    }
    if (url === '/api/v1/device/bindCodeList') return Promise.resolve({ data: { bindCodes: [] } })
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    devices.openBindCodeDialog({ deviceId: 1, deviceNo: 'watch-1', oldId: 1 })
    await flushPromises()
    const first = devices.deleteBindCode({ id: 11 })
    const second = devices.deleteBindCode({ id: 11 })
    expect(deletes).toBe(1)
    finishDelete({ data: { returnCode: '1111' } })
    await Promise.all([first, second])
    expect(deletes).toBe(1)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('ignores an alert detail response after its dialog has closed', async () => {
  let finish!: (value: unknown) => void
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/alertDetail') return new Promise(resolve => { finish = resolve })
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    const pending = devices.showAlert({ deviceId: 1, deviceNo: 'watch-1' }, 'rate')
    await flushPromises()
    devices.alertDialog.value.visible = false
    finish({ data: { hasAlert: 1, reason: 'late result' } })
    await pending
    expect(devices.alertDialog.value.reason).toBe('')
    expect(devices.alertDialog.value.visible).toBe(false)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('does not repeat a pending alert resolution and only locks the affected row', async () => {
  let complete!: (value: unknown) => void
  let writes = 0
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/resolveAlert') {
      writes++
      return new Promise(resolve => { complete = resolve })
    }
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  const row = { deviceId: 1, deviceNo: 'watch-1', kind: 'person', recordId: 14 }
  const another = { deviceId: 2, deviceNo: 'watch-2', kind: 'person', recordId: 15 }
  try {
    await flushPromises()
    const first = devices.resolveAlertRow(row)
    const duplicate = devices.resolveAlertRow(row)
    expect(writes).toBe(1)
    expect(devices.isResolvingAlert(row)).toBe(true)
    expect(devices.isResolvingAlert(another)).toBe(false)
    complete({ data: { returnCode: '1111' } })
    await Promise.all([first, duplicate])
    expect(devices.isResolvingAlert(row)).toBe(false)
    expect(writes).toBe(1)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('waits for queued device snapshots before reporting a refresh complete', async () => {
  const pendingReads: Array<(reply: unknown) => void> = []
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/list') return new Promise(resolve => { pendingReads.push(resolve) })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    expect(pendingReads).toHaveLength(1)
    let complete = false
    const refresh = devices.loadDevices().then(() => { complete = true })
    pendingReads[0]({ data: { devices: [{ deviceId: 1, deviceNo: 'previous' }], total: 1 } })
    await flushPromises()
    expect(pendingReads).toHaveLength(2)
    expect(complete).toBe(false)
    pendingReads[1]({ data: { devices: [{ deviceId: 2, deviceNo: 'current' }], total: 1 } })
    await refresh
    expect(complete).toBe(true)
    expect(devices.deviceList.value[0]?.deviceId).toBe(2)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('offers an explicit retry after alert detail fails without allowing an unverified resolution', async () => {
  let attempts = 0
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/alertDetail') {
      attempts++
      return attempts === 1
        ? Promise.reject({ code: 'ERR_NETWORK' })
        : Promise.resolve({ data: { hasAlert: 1, reason: '心率超出阈值', recordId: 8 } })
    }
    if (url === '/api/v1/device/list') return Promise.resolve({ data: { devices: [], total: 0 } })
    if (url === '/api/v1/device/alertCounts') return Promise.resolve({ data: { items: [], total: 0 } })
    if (url === '/api/v1/inviteCode/oldPersonList') return Promise.resolve({ data: { oldPeople: [] } })
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    await devices.showAlert({ deviceId: 3, deviceNo: 'watch-3' }, 'rate')
    expect(devices.alertDialog.value.hasAlert).toBe(-1)
    expect(devices.alertDialog.value.message).toContain('网络')
    devices.retryAlertDetail()
    await flushPromises()
    expect(attempts).toBe(2)
    expect(devices.alertDialog.value.hasAlert).toBe(1)
    expect(devices.alertDialog.value.reason).toBe('心率超出阈值')
    expect(devices.alertDialog.value.recordId).toBe(8)
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})
