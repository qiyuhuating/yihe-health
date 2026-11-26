import { afterEach, expect, it, vi } from 'vitest'
import { frameCadenceHz, sampleWebViewHz } from '@/utils/displayRefresh'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

it('reports browser frame cadence separately from display mode, even at 360 Hz', () => {
  expect(frameCadenceHz(Array.from({ length: 60 }, () => 1000 / 60))).toBe(60)
  expect(frameCadenceHz(Array.from({ length: 60 }, () => 1000 / 120))).toBe(120)
  expect(frameCadenceHz(Array.from({ length: 60 }, () => 1000 / 240))).toBe(240)
  expect(frameCadenceHz(Array.from({ length: 60 }, () => 1000 / 360))).toBe(360)
  expect(frameCadenceHz([...Array.from({ length: 40 }, () => 1000 / 240), 33, 33])).toBe(240)
  expect(frameCadenceHz([16.67, 16.67])).toBeNull()
})

it('does not schedule frames for a cancelled or hidden performance probe', async () => {
  const request = vi.spyOn(window, 'requestAnimationFrame')
  const aborted = new AbortController()
  aborted.abort()
  expect(await sampleWebViewHz(aborted.signal)).toBeNull()
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  expect(await sampleWebViewHz(new AbortController().signal)).toBeNull()
  expect(request).not.toHaveBeenCalled()
  hidden.mockRestore()
})

it('aborts a live sampling loop without leaving frame callbacks or timers', async () => {
  vi.useFakeTimers()
  const callbacks: FrameRequestCallback[] = []
  const cancel = vi.fn()
  vi.stubGlobal('requestAnimationFrame', vi.fn((cb: FrameRequestCallback) => {
    callbacks.push(cb)
    return callbacks.length
  }))
  vi.stubGlobal('cancelAnimationFrame', cancel)
  const controller = new AbortController()
  const pending = sampleWebViewHz(controller.signal)
  expect(callbacks).toHaveLength(1)
  callbacks[0](0)
  expect(callbacks).toHaveLength(2)
  controller.abort()
  await expect(pending).resolves.toBeNull()
  expect(cancel).toHaveBeenCalledWith(2)
  expect(vi.getTimerCount()).toBe(0)
})
