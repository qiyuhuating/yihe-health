import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { expect, it, vi } from 'vitest'
import axios from 'axios'
import { useDevices } from '@/composables/useDevices'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

function baseline(url: string): unknown {
  if (url === '/api/v1/device/bindCodeList') return { data: { bindCodes: [] } }
  if (url === '/api/v1/device/list') return { data: { devices: [], total: 0 } }
  if (url === '/api/v1/device/alertCounts') return { data: { items: [], total: 0 } }
  if (url === '/api/v1/inviteCode/oldPersonList') return { data: { oldPeople: [] } }
  return undefined
}

it('issues only one binding code for repeated clicks while the first write is in flight', async () => {
  let finishCreate!: (value: unknown) => void
  const createCalls: unknown[] = []
  vi.spyOn(axios, 'post').mockImplementation((url, body) => {
    if (url === '/api/v1/device/createBindCode') {
      createCalls.push(body)
      return new Promise(resolve => { finishCreate = resolve })
    }
    const reply = baseline(url)
    if (reply) return Promise.resolve(reply)
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  try {
    await flushPromises()
    devices.openBindCodeDialog({ deviceId: 1, deviceNo: 'watch-1', oldId: 1 })
    await flushPromises()
    const first = devices.createBindCode()
    await flushPromises()
    const second = devices.createBindCode()
    await flushPromises()
    expect(createCalls).toHaveLength(1)
    finishCreate({ data: { returnCode: '1111', code: 'ABC', expireTime: 'tomorrow' } })
    await Promise.all([first, second])
    expect(devices.newBindCode.value).toEqual({ code: 'ABC', expireTime: 'tomorrow' })
  } finally {
    wrapper.unmount()
    vi.restoreAllMocks()
  }
})

it('drops a device registration result after the view is gone', async () => {
  let finishCreate!: (value: unknown) => void
  vi.spyOn(axios, 'post').mockImplementation(url => {
    if (url === '/api/v1/device/create') return new Promise(resolve => { finishCreate = resolve })
    const reply = baseline(url)
    if (reply) return Promise.resolve(reply)
    throw new Error(`unexpected request: ${url}`)
  })
  let devices!: ReturnType<typeof useDevices>
  const wrapper = mount(defineComponent({ setup() { devices = useDevices(); return () => null } }))
  let unmounted = false
  try {
    await flushPromises()
    const pending = devices.createDevice()
    await flushPromises()
    wrapper.unmount()
    unmounted = true
    finishCreate({ data: { returnCode: '1111', deviceNo: 'watch-9', secret: 'secret' } })
    await pending
    expect(devices.createdDevice.value).toBeNull()
    expect(devices.creating.value).toBe(false)
  } finally {
    if (!unmounted) wrapper.unmount()
    vi.restoreAllMocks()
  }
})
