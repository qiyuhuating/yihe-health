import { afterEach, expect, it } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useTextSizePreference } from '@/composables/useTextSizePreference'

const Host = defineComponent({
  setup() { return useTextSizePreference() },
  template: '<div />'
})

afterEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('large-text', 'high-contrast')
})

it('toggles display classes and persists both preferences', () => {
  const host = mount(Host)
  try {
    const vm = host.vm as unknown as ReturnType<typeof useTextSizePreference>
    vm.setLargeText(true)
    vm.setHighContrast(true)
    expect(localStorage.getItem('large_text')).toBe('true')
    expect(localStorage.getItem('high_contrast')).toBe('true')
    expect(document.documentElement.classList.contains('high-contrast')).toBe(true)
  } finally { host.unmount() }
})

it('synchronizes cross-tab storage events and storage clearing', async () => {
  const host = mount(Host)
  try {
    localStorage.setItem('high_contrast', 'true')
    window.dispatchEvent(new StorageEvent('storage', { key: 'high_contrast', newValue: 'true', storageArea: localStorage }))
    await nextTick()
    expect(document.documentElement.classList.contains('high-contrast')).toBe(true)
    localStorage.clear()
    window.dispatchEvent(new StorageEvent('storage', { key: null, storageArea: localStorage }))
    await nextTick()
    expect(document.documentElement.classList.contains('high-contrast')).toBe(false)
  } finally { host.unmount() }
})
