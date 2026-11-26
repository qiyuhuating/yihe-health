import { afterEach, expect, it, vi } from 'vitest'
import { startVisiblePolling } from '@/composables/visiblePolling'

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

it('removes background timers and refreshes once when visibility returns', () => {
  vi.useFakeTimers()
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  const refresh = vi.fn()
  const stop = startVisiblePolling(refresh, 15_000)
  try {
    vi.advanceTimersByTime(15_000)
    expect(refresh).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(0)
    vi.advanceTimersByTime(600_000)
    expect(refresh).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(1)
    vi.advanceTimersByTime(15_000)
    expect(refresh).toHaveBeenCalledTimes(3)
  } finally { stop() }
  document.dispatchEvent(new Event('visibilitychange'))
  expect(vi.getTimerCount()).toBe(0)
  expect(refresh).toHaveBeenCalledTimes(3)
})

it('does not schedule an interval when mounted in a hidden document', () => {
  vi.useFakeTimers()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  const refresh = vi.fn()
  const stop = startVisiblePolling(refresh, 30_000)
  try {
    vi.advanceTimersByTime(600_000)
    expect(refresh).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  } finally { stop() }
})

it('coalesces rapid visibility changes without firing a request storm', () => {
  vi.useFakeTimers()
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  const refresh = vi.fn()
  const stop = startVisiblePolling(refresh, 15_000)
  try {
    for (let attempt = 0; attempt < 10; attempt++) {
      hidden.mockReturnValue(true)
      document.dispatchEvent(new Event('visibilitychange'))
      hidden.mockReturnValue(false)
      document.dispatchEvent(new Event('visibilitychange'))
      vi.advanceTimersByTime(50)
    }
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(1)
    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(1500)
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(1)
  } finally { stop() }
  expect(vi.getTimerCount()).toBe(0)
})

it('refreshes on first foreground return after a hidden mount', () => {
  vi.useFakeTimers()
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  const refresh = vi.fn()
  const stop = startVisiblePolling(refresh, 15_000, { minGapMs: 2_000 })
  try {
    vi.advanceTimersByTime(200)
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(1)
  } finally { stop() }
})
