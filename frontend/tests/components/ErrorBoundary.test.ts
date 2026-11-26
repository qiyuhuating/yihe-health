import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import ErrorBoundary from '@/components/ErrorBoundary.vue'

it('reports a contained failure with the global handler prefix instead of swallowing it', async () => {
  const logged = vi.spyOn(console, 'error').mockImplementation(() => {})
  const Broken = defineComponent(() => () => { throw new Error('reported failure') })
  const wrapper = mount(ErrorBoundary, { slots: { default: () => h(Broken) } })
  await nextTick()
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  expect(logged).toHaveBeenCalledWith('[app]', expect.any(String), expect.objectContaining({ message: 'reported failure' }))
  wrapper.unmount()
})

it('contains a child render failure and remounts it on retry', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  let broken = true
  const Child = defineComponent(() => () => {
    if (broken) throw new Error('render failure')
    return h('p', '已恢复')
  })
  const wrapper = mount(ErrorBoundary, { slots: { default: () => h(Child) } })
  await nextTick()
  expect(wrapper.get('[role="alert"]').text()).toContain('当前页面暂时无法显示')
  broken = false
  await wrapper.get('button').trigger('click')
  expect(wrapper.text()).toBe('已恢复')
  wrapper.unmount()
})

it('stops remounting a persistently broken child after three retries', async () => {
  vi.useFakeTimers()
  try {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const Broken = defineComponent(() => () => { throw new Error('still broken') })
    const wrapper = mount(ErrorBoundary, { slots: { default: () => h(Broken) } })
    await nextTick()
    for (let retry = 0; retry < 3; retry++) {
      if (retry > 0) {
        vi.advanceTimersByTime(1200)
        await nextTick()
      }
      await wrapper.get('button').trigger('click')
      await nextTick()
    }
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('连续重试仍未恢复')
    wrapper.unmount()
  } finally {
    vi.useRealTimers()
  }
})

it('keeps shell navigation mounted and clears a failure when the route key changes', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  const Broken = defineComponent(() => () => { throw new Error('bad child') })
  const Shell = defineComponent({
    props: { path: { type: String, required: true } },
    setup: props => () => h('main', [h('nav', '菜单'), h(ErrorBoundary, { key: props.path }, {
      default: () => props.path === '/broken' ? h(Broken) : h('p', '其他页面')
    })])
  })
  const wrapper = mount(Shell, { props: { path: '/broken' } })
  await nextTick()
  expect(wrapper.get('nav').text()).toBe('菜单')
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  await wrapper.setProps({ path: '/healthy' })
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('其他页面')
  wrapper.unmount()
})
