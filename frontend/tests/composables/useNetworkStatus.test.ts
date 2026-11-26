import { afterEach, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useNetworkStatus } from '@/composables/useNetworkStatus'

const Host = defineComponent({
  setup() { return useNetworkStatus() },
  template: '<div />'
})

afterEach(() => vi.restoreAllMocks())

it('reacts to offline and online events and stops listening on unmount', async () => {
  const online = vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true)
  const host = mount(Host)
  const vm = host.vm as unknown as ReturnType<typeof useNetworkStatus>
  expect(vm.isOnline).toBe(true)
  online.mockReturnValue(false)
  window.dispatchEvent(new Event('offline'))
  await nextTick()
  expect(vm.isOnline).toBe(false)
  online.mockReturnValue(true)
  window.dispatchEvent(new Event('online'))
  await nextTick()
  expect(vm.isOnline).toBe(true)
  host.unmount()
  online.mockReturnValue(false)
  window.dispatchEvent(new Event('offline'))
  await nextTick()
  expect(vm.isOnline).toBe(true)
})
