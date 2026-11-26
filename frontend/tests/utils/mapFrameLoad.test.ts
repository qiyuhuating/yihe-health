import { expect, it, vi } from 'vitest'
import { useMapFrameLoad } from '@/composables/useMapFrameLoad'

it('times out, retries, ignores the old frame and cancels the timer on close', () => {
  vi.useFakeTimers()
  try {
    const load = useMapFrameLoad()
    const first = document.createElement('iframe')
    const second = document.createElement('iframe')
    for (const frame of [first, second]) {
      frame.addEventListener('load', event => load.finishMapLoad(event, false))
      frame.addEventListener('error', event => load.finishMapLoad(event, true))
    }
    load.startMapLoad()
    load.frame.value = first
    vi.advanceTimersByTime(10_000)
    expect(load.mapLoadError.value).toBe(true)
    load.startMapLoad()
    load.frame.value = second
    first.dispatchEvent(new Event('load'))
    expect(load.mapLoading.value).toBe(true)
    second.dispatchEvent(new Event('load'))
    vi.advanceTimersByTime(10_000)
    expect(load.mapLoadError.value).toBe(false)
    expect(load.mapLoading.value).toBe(false)
    second.dispatchEvent(new Event('error'))
    expect(load.mapLoadError.value).toBe(true)
    load.startMapLoad()
    load.resetFrameLoad()
    vi.advanceTimersByTime(10_000)
    expect(load.mapLoadError.value).toBe(false)
    expect(load.mapLoading.value).toBe(false)
  } finally {
    vi.useRealTimers()
  }
})
