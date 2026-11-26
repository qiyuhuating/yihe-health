import { afterEach, describe, it, expect, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { awaitAbortable, createLatestRead } from '@/composables/latestRead'
import { readErrorMessage, READ_TIMEOUT_MESSAGE } from '@/api/httpContract'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

describe('cancelable latest read scheduler', () => {
  afterEach(() => { vi.useRealTimers() })

  it('coalesces a synchronous burst before starting the first request', async () => {
    const read = vi.fn(async (page: number) => page)
    const accept = vi.fn(), fail = vi.fn(), busy = vi.fn()
    const queue = createLatestRead(read, accept, fail, busy)
    const pending: Promise<void>[] = []
    for (let page = 1; page <= 1000; page++) pending.push(queue.run(page))
    await Promise.all(pending)
    expect(read.mock.calls).toEqual([[1000, expect.any(AbortSignal)]])
    expect(accept.mock.calls).toEqual([[1000, 1000]])
    expect(busy.mock.calls).toEqual([[true], [false]])
  })

  it('starts the latest request without waiting for a source that never settles', async () => {
    const read = vi.fn((page: number, _signal: AbortSignal) => page === 1 ? new Promise<number>(() => {}) : Promise.resolve(page))
    const accept = vi.fn(), busy = vi.fn()
    const queue = createLatestRead(read, accept, vi.fn(), busy)
    const old = queue.run(1)
    await flushPromises()
    const latest = queue.run(2)
    await Promise.all([old, latest])
    expect(read.mock.calls.map(([page]) => page)).toEqual([1, 2])
    expect(read.mock.calls[0]?.[1].aborted).toBe(true)
    expect(accept.mock.calls).toEqual([[2, 2]])
    expect(busy.mock.calls).toEqual([[true], [false]])
  })

  it('suppresses a late failure after the request has been replaced', async () => {
    const first = deferred<number>()
    const read = vi.fn((page: number) => page === 1 ? first.promise : Promise.resolve(page))
    const accept = vi.fn(), fail = vi.fn()
    const queue = createLatestRead(read, accept, fail, vi.fn())
    const old = queue.run(1)
    await flushPromises()
    const latest = queue.run(2)
    first.reject(new Error('stale failure'))
    await Promise.all([old, latest])
    expect(fail).not.toHaveBeenCalled()
    expect(accept.mock.calls).toEqual([[2, 2]])
  })

  it('reports the current rejection and allows a retry', async () => {
    const read = vi.fn<(page: number) => Promise<number>>().mockRejectedValueOnce(new Error('current')).mockResolvedValueOnce(3)
    const accept = vi.fn(), fail = vi.fn()
    const queue = createLatestRead(read, accept, fail, vi.fn())
    await queue.run(1)
    expect(fail).toHaveBeenCalledTimes(1)
    await queue.run(3)
    expect(accept.mock.calls).toEqual([[3, 3]])
  })

  it('lets an accept callback queue a corrected page', async () => {
    const read = vi.fn(async (page: number) => page)
    const accepted: number[] = []
    const queue = createLatestRead(read, page => {
      if (page === 9) queue.run(2)
      else accepted.push(page)
    }, vi.fn(), vi.fn())
    await queue.run(9)
    expect(read.mock.calls.map(([page]) => page)).toEqual([9, 2])
    expect(accepted).toEqual([2])
  })

  it('handles a synchronous reader failure and can start again', async () => {
    const read = vi.fn<(page: number) => Promise<number>>()
      .mockImplementationOnce(() => { throw new Error('synchronous') })
      .mockResolvedValueOnce(2)
    const accept = vi.fn(), fail = vi.fn()
    const queue = createLatestRead(read, accept, fail, vi.fn())
    await queue.run(1)
    await queue.run(2)
    expect(fail).toHaveBeenCalledTimes(1)
    expect(accept.mock.calls).toEqual([[2, 2]])
  })

  it('times out a never-settling read and restores the loading state', async () => {
    vi.useFakeTimers()
    try {
      const read = vi.fn((page: number) => page === 1 ? new Promise<number>(() => {}) : Promise.resolve(page))
      const accept = vi.fn(), fail = vi.fn(), busy = vi.fn()
      const queue = createLatestRead(read, accept, fail, busy, { timeoutMs: 10 })
      const pending = queue.run(1)
      await vi.advanceTimersByTimeAsync(10)
      await pending
      expect(fail).toHaveBeenCalledTimes(1)
      expect(readErrorMessage(fail.mock.calls[0][0])).toBe(READ_TIMEOUT_MESSAGE)
      expect(busy.mock.calls).toEqual([[true], [false]])
      await queue.run(2)
      expect(accept.mock.calls).toEqual([[2, 2]])
    } finally {
      vi.useRealTimers()
    }
  })

  it('cancels a read silently, clears busy state and can load again', async () => {
    vi.useFakeTimers()
    const first = deferred<number>()
    const read = vi.fn((_page: number, _signal: AbortSignal) => first.promise)
      .mockReturnValueOnce(first.promise)
      .mockResolvedValueOnce(2)
    const accept = vi.fn(), fail = vi.fn(), busy = vi.fn()
    const queue = createLatestRead(read, accept, fail, busy)
    const pending = queue.run(1)
    await flushPromises()
    const signal = read.mock.calls[0]?.[1]
    queue.cancel()
    await pending
    expect(signal?.aborted).toBe(true)
    expect(fail).not.toHaveBeenCalled()
    expect(busy.mock.calls).toEqual([[true], [false]])
    await queue.run(2)
    first.resolve(1)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(accept.mock.calls).toEqual([[2, 2]])
    expect(fail).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not start a read when disposed before the scheduled drain', async () => {
    const read = vi.fn(), busy = vi.fn()
    const queue = createLatestRead(read, vi.fn(), vi.fn(), busy)
    const pending = queue.run(1)
    queue.dispose()
    queue.dispose()
    await pending
    expect(read).not.toHaveBeenCalled()
    expect(busy.mock.calls).toEqual([[true], [false]])
  })

  it('settles a waiting run on disposal without accepting late responses', async () => {
    const first = deferred<number>()
    const read = vi.fn(() => first.promise), accept = vi.fn(), fail = vi.fn()
    const queue = createLatestRead<number, number>(read, accept, fail, vi.fn())
    const pending = queue.run(1)
    await flushPromises()
    queue.run(2)
    queue.dispose()
    await pending
    first.reject(new Error('late failure'))
    await flushPromises()
    await queue.run(3)
    expect(read).toHaveBeenCalledTimes(1)
    expect(accept).not.toHaveBeenCalled()
    expect(fail).not.toHaveBeenCalled()
  })
  it('coalesces identical in-flight queries without aborting slow responses', async () => {
    const response = deferred<number>()
    const read = vi.fn((_page: number, _signal: AbortSignal) => response.promise)
    const accept = vi.fn(), fail = vi.fn(), busy = vi.fn()
    const queue = createLatestRead(read, accept, fail, busy, { key: page => String(page) })
    const initial = queue.run(3)
    await flushPromises()
    const duplicate = queue.run(3)
    const signal = read.mock.calls[0]?.[1]
    expect(read).toHaveBeenCalledTimes(1)
    expect(signal?.aborted).toBe(false)
    response.resolve(27)
    await Promise.all([initial, duplicate])
    expect(accept.mock.calls).toEqual([[27, 3]])
    expect(busy.mock.calls).toEqual([[true], [false]])
  })

  it('replaces an in-flight query when its key changes', async () => {
    const stalled = deferred<number>()
    const read = vi.fn((page: number) => page === 1 ? stalled.promise : Promise.resolve(page))
    const accept = vi.fn()
    const queue = createLatestRead(read, accept, vi.fn(), vi.fn(), { key: page => String(page) })
    const first = queue.run(1)
    await flushPromises()
    const second = queue.run(2)
    await Promise.all([first, second])
    expect(read.mock.calls.map(([page]) => page)).toEqual([1, 2])
    expect(accept.mock.calls).toEqual([[2, 2]])
  })

  it('allows an identical query immediately after cancel', async () => {
    const read = vi.fn((page: number) => page === 1 && read.mock.calls.length === 1
      ? new Promise<number>(() => {}) : Promise.resolve(page))
    const accept = vi.fn()
    const queue = createLatestRead(read, accept, vi.fn(), vi.fn(), { key: page => String(page) })
    const first = queue.run(1)
    await flushPromises()
    queue.cancel()
    const second = queue.run(1)
    await Promise.all([first, second])
    expect(read).toHaveBeenCalledTimes(2)
    expect(accept.mock.calls).toEqual([[1, 1]])
  })

})

it('observes rejection of an already-aborted source promise', async () => {
  const controller = new AbortController()
  controller.abort()
  const source = deferred<number>()
  await expect(awaitAbortable(source.promise, controller.signal)).rejects.toThrow('read aborted')
  source.reject(new Error('late aborted transport failure'))
  await flushPromises()
})
