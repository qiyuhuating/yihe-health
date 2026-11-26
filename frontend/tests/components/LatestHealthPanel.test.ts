import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { expect, it, vi } from 'vitest'
import axios from 'axios'
import LatestHealthPanel from '@/components/LatestHealthPanel.vue'

const Button = defineComponent({ emits: ['click'], template: '<button @click="$emit(\'click\')"><slot /></button>' })
const mountPanel = () => mount(LatestHealthPanel, { global: { stubs: { ElButton: Button } } })
const sample = { recordId: 'r', source: 'watch', sourceDevice: 'A', metric: 'steps', semantics: 'daily_total', unit: 'count', value: 0, start: '2026-10-20T09:00:00Z', end: '2026-10-20T09:00:00Z' }

it('shows separate sources, zero values and original sampling intervals', async () => {
  const post = vi.spyOn(axios, 'post').mockResolvedValue({ data: { returnCode: '1111', oldId: 1, items: [
    { sampleId: 'a', deviceId: 1, bindingVersion: 3, sample },
    { sampleId: 'b', deviceId: 2, bindingVersion: 1, sample: { ...sample, source: 'other-watch', sourceDevice: 'B', value: 50 } },
    { sampleId: 'c', deviceId: 1, bindingVersion: 3, sample: { ...sample, metric: 'sleep_stage', semantics: 'interval', unit: 'min', stage: 'deep', value: 60, end: '2026-10-20T10:00:00Z' } }
  ] } })
  const view = mountPanel()
  try {
    await flushPromises()
    const cards = view.findAll('.latest-list li')
    expect(cards).toHaveLength(3)
    expect(cards[0].get('.latest-value').text()).toBe('0 步')
    expect(cards[0].text()).toContain('日累计快照')
    expect(cards[0].text()).toContain('watch · A · 绑定版本 3')
    expect(cards[1].text()).toContain('other-watch · B')
    expect(cards[2].text()).toContain('采样区间：')
    expect(cards[2].text()).toContain('深睡')
    expect(cards[2].text()).toContain(new Date(sample.start).toLocaleString('zh-CN', { timeZoneName: 'short' }))
    expect(view.text()).not.toContain('今日步数')
    expect(post.mock.calls[0][0]).toBe('/api/v1/health/latest')
    expect(post.mock.calls[0][1]).toEqual({})
  } finally { view.unmount() }
})

it('shows read failure and retries to an explicit empty state', async () => {
  vi.spyOn(axios, 'post').mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ data: { returnCode: '1111', items: [] } })
  const view = mountPanel()
  try {
    await flushPromises()
    expect(view.get('[role="alert"]').text()).toContain('暂时无法加载')
    await view.get('button').trigger('click')
    await flushPromises()
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.text()).toContain('暂无健康采样')
    expect(view.get('.latest-health').attributes('aria-busy')).toBe('false')
  } finally { view.unmount() }
})

it('labels native sensor timestamps as application observations rather than device UTC', async () => {
  vi.spyOn(axios, 'post').mockResolvedValue({ data: { returnCode: '1111', items: [
    { sampleId: 'native', deviceId: 1, bindingVersion: 1, sample: { ...sample, source: 'yihe-harmonyos', semantics: 'interval', metadata: { timestampBasis: 'app-received' } } }
  ] } })
  const view = mountPanel()
  try {
    await flushPromises()
    expect(view.text()).toContain('鸿蒙手表（应用上报）')
    expect(view.text()).toContain('应用观测区间')
    expect(view.text()).not.toContain('日累计快照')
  } finally { view.unmount() }
})

it('coalesces repeated refreshes while the same read is in flight', async () => {
  const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
  const view = mountPanel()
  try {
    await flushPromises()
    const signal = post.mock.calls[0][2]?.signal
    await view.get('button').trigger('click')
    await view.get('button').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
    expect(signal?.aborted).toBe(false)
  } finally { view.unmount() }
})

it('cancels an unfinished read on leaving the page', async () => {
  const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
  const view = mountPanel()
  await flushPromises()
  const signal = post.mock.calls[0][2]?.signal
  view.unmount()
  expect(signal?.aborted).toBe(true)
})
