import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import axios from 'axios'
import CareOperationsPanel from '@/components/CareOperationsPanel.vue'
import { authState } from '@/auth'
import { REQUEST_TIMEOUT_MS } from '@/api/client'

beforeEach(() => { authState.account = { role: 'ADMIN', id: 1, duty: 'supervisor' } })
afterEach(() => { authState.account = null; vi.restoreAllMocks() })

const Button = defineComponent({ emits: ['click'], template: '<button @click="$emit(\'click\')"><slot /></button>' })
const Select = defineComponent({ emits: ['change'], template: '<button @click="$emit(\'change\', \'caregiver\')">更改岗位</button>' })

it.each(['staff', 'assignDuty', 'accessAudit'])('renders %s server errors without losing the error state', async endpoint => {
  vi.spyOn(axios, 'post').mockImplementation(async url => {
    if (url === '/api/v1/care/' + endpoint) throw { response: { status: 409, data: { returnCode: '0000', code: 'CONFLICT', message: '岗位记录已变化', reference: 'care-1' } } }
    return { data: url.endsWith('/staff') ? { staff: [{ id: 1, name: '值守', duty: 'supervisor' }] } : {} }
  })
  const view = mount(CareOperationsPanel, { global: { stubs: { ElButton: Button, ElSelect: Select, ElOption: true, SecurityPanel: true } } })
  try {
    await flushPromises()
    if (endpoint === 'assignDuty') await view.findAll('button').find(b => b.text() === '更改岗位')!.trigger('click')
    if (endpoint === 'accessAudit') await view.findAll('button').find(b => b.text() === '查看敏感数据访问审计')!.trigger('click')
    await flushPromises()
    expect(view.get('.care-error').text()).toBe('岗位记录已变化（编号：care-1）')
  } finally { view.unmount(); vi.restoreAllMocks() }
})

it('ignores a late staff response after the component unmounts', async () => {
  let finishStaff!: (value: unknown) => void
  vi.spyOn(axios, 'post').mockImplementation(async url => {
    if (url === '/api/v1/care/staff') return new Promise(resolve => { finishStaff = resolve })
    return { data: { primaryEngine: 'mysql', workerHealthy: true } }
  })
  const view = mount(CareOperationsPanel, {
    global: { stubs: { ElButton: Button, ElSelect: Select, ElOption: true, SecurityPanel: true } }
  })
  await flushPromises()
  view.unmount()
  finishStaff({ data: { staff: [{ id: 1, name: 'late', duty: 'caregiver' }] } })
  await flushPromises()
  expect(axios.post).toHaveBeenCalledWith('/api/v1/care/staff', {}, { withCredentials: true, signal: undefined, timeout: REQUEST_TIMEOUT_MS })
  vi.restoreAllMocks()
})
