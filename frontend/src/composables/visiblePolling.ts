// Periodic reads run only while the document is visible. Callers retain
// their own request deadlines, cancellation and busy-state policies.
export interface VisiblePollingOptions {
  /** Minimum elapsed time before returning to a tab can trigger an extra read. */
  minGapMs?: number
}

export function startVisiblePolling(
  refresh: () => void,
  intervalMs: number,
  options: VisiblePollingOptions = {}
): () => void {
  const minGapMs = options.minGapMs ?? Math.min(intervalMs, 1500)
  let timer: ReturnType<typeof setInterval> | undefined
  let lastRefreshAt: number | undefined

  function clearTimer() {
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
  }

  function runRefresh() {
    lastRefreshAt = Date.now()
    refresh()
  }

  function tick() {
    if (!document.hidden) runRefresh()
  }

  function onVisibilityChange() {
    clearTimer()
    if (document.hidden) return
    timer = setInterval(tick, intervalMs)
    // Foreground return refreshes unless a recent dispatch already did so.
    // A hidden mount has no dispatch to suppress its first refresh.
    const now = Date.now()
    if (lastRefreshAt === undefined || now - lastRefreshAt >= minGapMs) {
      runRefresh()
    }
  }

  if (!document.hidden) timer = setInterval(tick, intervalMs)
  document.addEventListener('visibilitychange', onVisibilityChange)
  return () => {
    clearTimer()
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }
}
