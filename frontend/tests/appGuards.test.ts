import { expect, it, vi } from 'vitest'
import { APP_TITLE, documentTitle, isChunkLoadError, recoverFromChunkError } from '@/appGuards'

const chunkError = new TypeError('Failed to fetch dynamically imported module: /assets/Residents-1a2b.js')
const memory = () => {
  const data = new Map<string, string>()
  return { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => { data.set(k, v) } }
}

it('recognises chunk loading failures only', () => {
  expect(isChunkLoadError(chunkError)).toBe(true)
  expect(isChunkLoadError(new Error('Importing a module script failed.'))).toBe(true)
  expect(isChunkLoadError(new Error('network down'))).toBe(false)
  expect(isChunkLoadError(undefined)).toBe(false)
})

it('reloads once per cooldown window and ignores other errors', () => {
  const reload = vi.fn()
  const storage = memory()
  expect(recoverFromChunkError(new Error('boom'), reload, 1_000, storage)).toBe(false)
  expect(recoverFromChunkError(chunkError, reload, 1_000, storage)).toBe(true)
  expect(recoverFromChunkError(chunkError, reload, 30_000, storage)).toBe(false)
  expect(recoverFromChunkError(chunkError, reload, 70_000, storage)).toBe(true)
  expect(reload).toHaveBeenCalledTimes(2)
})

it('does not reload when storage is unavailable', () => {
  const reload = vi.fn()
  const broken = { getItem: () => { throw new Error('blocked') }, setItem: () => undefined }
  expect(recoverFromChunkError(chunkError, reload, 1_000, broken)).toBe(false)
  expect(reload).not.toHaveBeenCalled()
})

it('never reloads without a place to record the attempt', () => {
  const reload = vi.fn()
  expect(recoverFromChunkError(chunkError, reload, 1_000, null)).toBe(false)
  expect(reload).not.toHaveBeenCalled()
})

it('builds the document title from the route title', () => {
  expect(documentTitle('老人档案')).toBe(`老人档案 · ${APP_TITLE}`)
  expect(documentTitle(undefined)).toBe(APP_TITLE)
})
