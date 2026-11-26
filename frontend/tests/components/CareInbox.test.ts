import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest'
import CareInbox from '@/components/CareInbox.vue'
import store from '@/store'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('axios', async (importOriginal) => ({ ...await importOriginal<typeof import('axios')>(), default: { post } }))
const Pagination = defineComponent({ name: 'Pagination', emits: ['current-change'], template: '<nav />' })
const Button = defineComponent({ emits: ['click'], template: '<button @click="$emit(\'click\')"><slot /></button>' })
const Dialog = defineComponent({ props: ['modelValue'], emits: ['close', 'update:modelValue'], template: '<section v-if="modelValue" class="test-dialog"><slot /><slot name="footer" /><button class="close-dialog" @click="$emit(\'update:modelValue\', false); $emit(\'close\')">关闭</button></section>' })
const fixture = (name: string, total = 100) => ({ data: { items: [{ id: name, oldPersonName: name, state: 'received' }], total, duty: 'supervisor', actorId: 7 } })
function mountInbox() {
  return mount(CareInbox, { global: {
    stubs: { 'el-pagination': Pagination, 'el-select': true, 'el-option': true, 'el-button': Button, 'el-dialog': Dialog, 'el-input': true, CareOperationsPanel: true },
    directives: { loading() {} }
  } })
}

describe('care task read scheduling', () => {
  beforeEach(() => { post.mockReset() })
  afterEach(() => { vi.useRealTimers() })

  it('refreshes the care inbox on an external alert revision', async () => {
    post.mockResolvedValueOnce(fixture('旧任务')).mockResolvedValueOnce(fixture('新求助'))
    const view = mountInbox()
    await flushPromises()
    store.notifyAlertsChanged()
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(2)
    expect(view.text()).toContain('新求助')
    expect(view.text()).not.toContain('旧任务')
    view.unmount()
  })

  it('refreshes tasks on tab visibility restoration and unregisters the listener', async () => {
    const visibility = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    try {
      post.mockResolvedValueOnce(fixture('旧任务')).mockResolvedValueOnce(fixture('最新任务'))
      const view = mountInbox()
      await flushPromises()
      expect(view.text()).toContain('旧任务')
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(post).toHaveBeenCalledTimes(1)

      visibility.mockReturnValue(false)
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(post).toHaveBeenCalledTimes(2)
      expect(view.text()).toContain('最新任务')
      expect(view.text()).not.toContain('旧任务')

      view.unmount()
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(post).toHaveBeenCalledTimes(2)
    } finally {
      visibility.mockRestore()
    }
  })

  it('aborts a hung task page on timeout and can poll successfully afterwards', async () => {
    vi.useFakeTimers()
    post.mockReturnValueOnce(new Promise(() => {})).mockResolvedValueOnce(fixture('恢复任务'))
    const view = mountInbox()
    await vi.advanceTimersByTimeAsync(10000)
    expect(view.get('.care-inbox').attributes('aria-busy')).toBe('false')
    expect(view.text()).toContain('查询超时')
    expect(post.mock.calls[0][2].signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    expect(view.text()).toContain('恢复任务')
    expect(view.text()).not.toContain('查询超时')
    expect(post.mock.calls.every(([path]) => path === '/api/v1/care/incidents')).toBe(true)
    view.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('aborts the active task request immediately when unmounted', async () => {
    post.mockReturnValue(new Promise(() => {}))
    const view = mountInbox()
    await flushPromises()
    const signal = post.mock.calls[0][2].signal
    expect(signal.aborted).toBe(false)
    view.unmount()
    expect(signal.aborted).toBe(true)
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('replaces a hung history read and cancels another read when its dialog closes', async () => {
    let finishOld!: (value: unknown) => void
    post.mockResolvedValueOnce({ data: { ...fixture('first').data, items: [
      { id: 'first', oldPersonName: '第一位', state: 'received' },
      { id: 'second', oldPersonName: '第二位', state: 'received' }
    ] } })
      .mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ data: { events: [{ action: '最新履历' }], total: 1 } })
      .mockReturnValueOnce(new Promise(() => {}))
    const view = mountInbox()
    await flushPromises()
    const buttons = view.findAll('.care-task-actions button').filter(button => button.text() === '处置记录')
    await buttons[0].trigger('click')
    await flushPromises()
    await buttons[1].trigger('click')
    await flushPromises()
    expect(post.mock.calls[1][2].signal.aborted).toBe(true)
    expect(post.mock.calls[2][1]).toMatchObject({ id: 'second', page: 1 })
    expect(post.mock.calls[2][0]).toBe('/api/v1/care/events')
    expect(view.text()).toContain('最新履历')
    finishOld({ data: { events: [{ action: '过期履历' }], total: 99 } })
    await flushPromises()
    expect(view.text()).not.toContain('过期履历')
    await buttons[0].trigger('click')
    await flushPromises()
    const signal = post.mock.calls[3][2].signal
    await view.get('.close-dialog').trigger('click')
    expect(signal.aborted).toBe(true)
    await flushPromises()
    expect(view.find('.test-dialog').exists()).toBe(false)
    view.unmount()
  })

  it('times out history and aborts a later history request on unmount', async () => {
    vi.useFakeTimers()
    post.mockResolvedValueOnce(fixture('历史任务')).mockReturnValue(new Promise(() => {}))
    const view = mountInbox()
    await vi.advanceTimersByTimeAsync(0)
    const button = view.findAll('.care-task-actions button').find(button => button.text() === '处置记录')!
    await button.trigger('click')
    await vi.advanceTimersByTimeAsync(10000)
    expect(view.text()).toContain('查询超时')
    expect(view.text()).not.toContain('正在读取记录')
    expect(post.mock.calls[1][2].signal.aborted).toBe(true)
    await button.trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    const signal = post.mock.calls[2][2].signal
    view.unmount()
    expect(signal.aborted).toBe(true)
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps delivered notifications visibly unclaimed until a person acknowledges', async () => {
    post.mockResolvedValueOnce({ data: { items: [{ id: 'delivery-only', oldPersonName: '送达待接单', state: 'notified', notificationState: 'delivered', acknowledgedBy: null }], total: 1, duty: 'supervisor', actorId: 7 } })
    const view = mountInbox()
    await flushPromises()
    expect(view.text()).toContain('已确认送达')
    expect(view.text()).toContain('尚未接单')
    expect(view.text()).not.toContain('已复核关闭')
    view.unmount()
  })

  it('discards a superseded page and loads the newest requested page', async () => {
    let second!: (value: unknown) => void
    let third!: (value: unknown) => void
    post.mockResolvedValueOnce(fixture('初始任务'))
      .mockReturnValueOnce(new Promise(resolve => { second = resolve }))
      .mockReturnValueOnce(new Promise(resolve => { third = resolve }))
    const view = mountInbox()
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 2)
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 3)
    second(fixture('过期任务'))
    await flushPromises()
    expect(view.text()).not.toContain('过期任务')
    expect(post.mock.calls.map(([, body]) => body.page)).toEqual([1, 2, 3])
    third(fixture('最新任务'))
    await flushPromises()
    expect(view.text()).toContain('最新任务')
    expect(view.get('.care-inbox').attributes('aria-busy')).toBe('false')
    view.unmount()
  })

  it('does not leave loading stuck when a superseded page never resolves', async () => {
    post.mockResolvedValueOnce(fixture('初始任务'))
      .mockReturnValueOnce(new Promise(() => {}))
      .mockResolvedValueOnce(fixture('恢复成功'))
    const view = mountInbox()
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 2)
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 3)
    await flushPromises()
    expect(post.mock.calls.map(([, body]) => body.page)).toEqual([1, 2, 3])
    expect(view.text()).toContain('恢复成功')
    expect(view.get('.care-inbox').attributes('aria-busy')).toBe('false')
    view.unmount()
  })

  it('queues the last available page when records have been removed', async () => {
    post.mockResolvedValueOnce(fixture('初始任务'))
      .mockResolvedValueOnce({ ...fixture('越界响应', 21), data: { ...fixture('越界响应', 21).data, items: [] } })
      .mockResolvedValueOnce(fixture('尾页任务', 21))
    const view = mountInbox()
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 5)
    await flushPromises()
    expect(post.mock.calls.map(([, body]) => body.page)).toEqual([1, 5, 2])
    expect(view.text()).toContain('尾页任务')
    expect(view.get('.care-inbox').attributes('aria-busy')).toBe('false')
    view.unmount()
  })

  it('does not start a queued page after the component is unmounted', async () => {
    let second!: (value: unknown) => void
    post.mockResolvedValueOnce(fixture('初始任务')).mockReturnValueOnce(new Promise(resolve => { second = resolve }))
    const view = mountInbox()
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 2)
    await flushPromises()
    view.findComponent(Pagination).vm.$emit('current-change', 3)
    view.unmount()
    second(fixture('迟到任务'))
    await flushPromises()
    expect(post).toHaveBeenCalledTimes(2)
  })
})

it('only offers verification to a supervisor who is neither assignee nor resolver', async () => {
  post.mockReset()
  post.mockResolvedValueOnce({ data: {
    items: [
      { id: 'assigned', oldPersonName: 'assigned', state: 'resolved', acknowledgedBy: '7', resolvedBy: '8' },
      { id: 'resolved', oldPersonName: 'resolved', state: 'review_overdue', acknowledgedBy: '8', resolvedBy: '7' },
      { id: 'independent', oldPersonName: 'independent', state: 'resolved', acknowledgedBy: '8', resolvedBy: '9' }
    ], total: 3, duty: 'supervisor', actorId: 7
  } })
  const view = mountInbox()
  try {
    await flushPromises()
    const tasks = view.findAll('.care-task')
    expect(tasks).toHaveLength(3)
    expect(tasks.map(task => task.findAll('.care-task-actions button').some(button => button.text() === '复核关闭'))).toEqual([false, false, true])
  } finally { view.unmount() }
})

it('discards a pre-commit list response and obtains a new snapshot after a care write', async () => {
  post.mockReset()
  let finishOld!: (value: unknown) => void
  const initial = fixture('resident', 1)
  const updated = { data: { ...initial.data, items: [{ ...initial.data.items[0], state: 'acknowledged', acknowledgedBy: '7' }] } }
  post.mockResolvedValueOnce(initial)
    .mockReturnValueOnce(new Promise(resolve => { finishOld = resolve }))
    .mockResolvedValueOnce({ data: { returnCode: '1111' } })
    .mockResolvedValueOnce(updated)
  const view = mountInbox()
  try {
    await flushPromises()
    store.notifyAlertsChanged()
    await flushPromises()
    const oldSignal = post.mock.calls[1][2].signal
    await view.findAll('.care-task-actions button').find(button => button.text() === '接单')!.trigger('click')
    await view.findAll('.test-dialog button').find(button => button.text() === '确认记录')!.trigger('click')
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(post.mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/care/incidents', '/api/v1/care/incidents', '/api/v1/care/transition', '/api/v1/care/incidents'
    ])
    expect(view.findAll('.care-task-actions button').some(button => button.text() === '开始处置')).toBe(true)
    finishOld(initial)
    await flushPromises()
    expect(view.findAll('.care-task-actions button').some(button => button.text() === '接单')).toBe(false)
  } finally { view.unmount() }
})

it('reconciles a lost transition response without automatically submitting again', async () => {
  post.mockReset()
  const initial = fixture('resident', 1)
  const updated = { data: { ...initial.data, items: [{ ...initial.data.items[0], state: 'acknowledged', acknowledgedBy: '7' }] } }
  post.mockResolvedValueOnce(initial)
    .mockRejectedValueOnce({ code: 'ECONNABORTED' })
    .mockResolvedValueOnce(updated)
  const view = mountInbox()
  try {
    await flushPromises()
    await view.findAll('.care-task-actions button').find(button => button.text() === '接单')!.trigger('click')
    await view.findAll('.test-dialog button').find(button => button.text() === '确认记录')!.trigger('click')
    await flushPromises()
    expect(post.mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/care/incidents', '/api/v1/care/transition', '/api/v1/care/incidents'
    ])
    expect(view.get('.test-dialog [role="alert"]').text()).toContain('先核对')
    expect(view.get('.test-dialog [role="status"]').text()).toContain('已接单')
    const confirm = view.findAll('.test-dialog button').find(button => button.text() === '确认记录')!
    expect(confirm.attributes('disabled')).toBeDefined()
    await confirm.trigger('click')
    expect(post.mock.calls.filter(([path]) => path === '/api/v1/care/transition')).toHaveLength(1)
    expect(view.findAll('.care-task-actions button').some(button => button.text() === '接单')).toBe(false)
  } finally { view.unmount() }
})
