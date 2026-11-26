const CHUNK_ERROR = /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS/i
const RELOAD_KEY = 'care.chunk-reload-at'
const RELOAD_COOLDOWN_MS = 60_000

export const APP_TITLE = '颐和 · 智慧养老管理平台'

export function isChunkLoadError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  return CHUNK_ERROR.test(message)
}

// Limit chunk-load recovery to one refresh per cooldown to avoid reload loops.
export function recoverFromChunkError(
  error: unknown,
  reload: () => void = () => window.location.reload(),
  now: number = Date.now(),
  storage: Pick<Storage, 'getItem' | 'setItem'> | null = safeSession()
): boolean {
  if (!isChunkLoadError(error) || !storage) return false
  try {
    const saved = storage.getItem(RELOAD_KEY)
    if (saved != null && now - Number(saved) < RELOAD_COOLDOWN_MS) return false
    storage.setItem(RELOAD_KEY, String(now))
  } catch {
    return false
  }
  reload()
  return true
}

export function documentTitle(pageTitle: unknown): string {
  return typeof pageTitle === 'string' && pageTitle ? `${pageTitle} · ${APP_TITLE}` : APP_TITLE
}

function safeSession(): Storage | null {
  try { return window.sessionStorage } catch { return null }
}
