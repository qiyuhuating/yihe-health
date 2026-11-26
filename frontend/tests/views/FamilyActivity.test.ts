import { flushPromises, shallowMount } from '@vue/test-utils'
import axios from 'axios'
import { expect, it, vi } from 'vitest'
import FamilyActivity from '@/views/FamilyActivity.vue'

it.each([
  [0, '0'],
  ['0', '0'],
  [8, '8'],
  [null, '暂无数据'],
  [undefined, '暂无数据']
])('distinguishes reported sleep %s from missing data in family cards', async (sleep, expected) => {
  vi.spyOn(axios, 'post').mockResolvedValue({ data: { activityRecords: [{ oldId: 1, name: '长者', step: 0, sleep }] } })
  const view = shallowMount(FamilyActivity, {
    global: {
      stubs: { ElTable: true, ElTableColumn: true, ElButton: true, ElAlert: true, ElDialog: true },
      directives: { loading: () => {} }
    }
  })
  try {
    await flushPromises()
    const fields = view.findAll('.family-record-card dd')
    expect(fields[0].text()).toBe('0 步')
    expect(fields[1].text()).toBe(expected)
  } finally {
    view.unmount()
  }
})
