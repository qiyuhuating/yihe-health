import { readTimeoutError } from '../api/httpContract'

// Read-only queue: an abandoned transport must never delay or overwrite newer state.
export function awaitAbortable<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    if (signal.aborted) {
      // The caller has already created this promise. Observe its eventual
      // rejection even when the transport was cancelled before wrapping it.
      void promise.catch(() => {})
      reject(new Error('read aborted'))
      return
    }
    const cleanup = () => signal.removeEventListener('abort', onAbort)
    const onAbort = () => { cleanup(); reject(new Error('read aborted')) }
    signal.addEventListener('abort', onAbort, { once: true })
    promise.then(value => { cleanup(); resolve(value) }, error => { cleanup(); reject(error as Error) })
  })
}

export function createLatestRead<P, T>(
  read: (params: P, signal: AbortSignal) => Promise<T>,
  accept: (value: T, params: P) => void,
  reject: (error: unknown) => void,
  busy: (value: boolean) => void,
  options: { timeoutMs?: number; key?: (params: P) => string } = {}
) {
  const timeoutMs = options.timeoutMs ?? 10_000
  let revision = 0
  let pending: { params: P; revision: number } | undefined
  let active: Promise<void> | undefined
  let controller: AbortController | undefined
  let currentKey: string | undefined
  let disposed = false

  async function drain() {
    try {
      while (pending && !disposed) {
        const job = pending
        pending = undefined
        currentKey = options.key?.(job.params)
        const current = new AbortController()
        controller = current
        let timedOut = false
        const timeout = timeoutMs > 0 ? setTimeout(() => {
          timedOut = true
          current.abort()
        }, timeoutMs) : undefined
        try {
          const value = await awaitAbortable(
            Promise.resolve().then(() => {
              if (current.signal.aborted) throw new Error('read aborted')
              return read(job.params, current.signal)
            }), current.signal)
          if (!disposed && job.revision === revision) accept(value, job.params)
        } catch (error) {
          if (!disposed && job.revision === revision) {
            if (timedOut) reject(readTimeoutError())
            else if (!current.signal.aborted) reject(error)
          }
        } finally {
          if (timeout !== undefined) clearTimeout(timeout)
          if (controller === current) controller = undefined
          currentKey = undefined
        }
      }
    } finally {
      active = undefined
      if (!disposed) busy(false)
    }
  }

  function run(params: P): Promise<void> {
    if (disposed) return Promise.resolve()
    const key = options.key?.(params)
    if (key !== undefined && active && (pending ? options.key?.(pending.params) === key : controller !== undefined && !controller.signal.aborted && currentKey === key)) {
      return active
    }
    pending = { params, revision: ++revision }
    controller?.abort()
    if (!active) {
      active = Promise.resolve().then(drain)
      busy(true)
    }
    return active
  }

  function cancel() {
    if (disposed) return
    pending = undefined
    revision++
    controller?.abort()
  }

  function dispose() {
    if (disposed) return
    disposed = true
    pending = undefined
    revision++
    controller?.abort()
    if (active) busy(false)
  }

  return { run, cancel, dispose }
}
