import { invoke, isTauri } from '@tauri-apps/api/core'

// The OS mode is available from the desktop shell; web pages cannot read it.
export async function hostMonitorHz(): Promise<number | null> {
  if (!isTauri()) return null
  const hz = await invoke<number>('monitor_refresh_rate')
  return Number.isFinite(hz) && hz > 1 ? hz : null
}

// Median frame interval reduces sensitivity to occasional skipped vsyncs.
// It measures the browser's *idle frame cadence*, never the actual display mode.
export function frameCadenceHz(intervals: readonly number[]): number | null {
  const valid = intervals.filter(delta => Number.isFinite(delta) && delta >= 1 && delta <= 100)
  if (valid.length < 20) return null
  const sorted = valid.sort((a, b) => a - b)
  const middle = Math.floor(sorted.length / 2)
  const median = sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2
  return Math.round(1000 / median)
}

// Short, on-demand sampler. No RAF loop survives an abort, an invisible tab,
// or the deadline if the browser throttles animation callbacks.
export function sampleWebViewHz(signal: AbortSignal): Promise<number | null> {
  if (signal.aborted || document.hidden) return Promise.resolve(null)
  return new Promise(resolve => {
    const intervals: number[] = []
    let previous: number | undefined
    let first: number | undefined
    let frame = 0
    let settled = false
    const deadline = window.setTimeout(() => finish(null), 2500)

    function finish(rate: number | null) {
      if (settled) return
      settled = true
      cancelAnimationFrame(frame)
      clearTimeout(deadline)
      signal.removeEventListener('abort', onAbort)
      document.removeEventListener('visibilitychange', onVisibility)
      resolve(rate)
    }
    function onAbort() { finish(null) }
    function onVisibility() { if (document.hidden) finish(null) }
    function step(timestamp: number) {
      if (settled) return
      if (first === undefined) first = timestamp
      if (previous !== undefined) intervals.push(timestamp - previous)
      previous = timestamp
      if (timestamp - first >= 650) {
        finish(frameCadenceHz(intervals))
        return
      }
      frame = requestAnimationFrame(step)
    }
    signal.addEventListener('abort', onAbort, { once: true })
    document.addEventListener('visibilitychange', onVisibility)
    frame = requestAnimationFrame(step)
  })
}
