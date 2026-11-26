import { afterEach, expect, it, vi } from 'vitest'
import { copyText } from '@/utils/clipboard'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); document.body.replaceChildren() })

it('uses the native clipboard without adding an input', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  await copyText('invite')
  expect(writeText).toHaveBeenCalledWith('invite')
  expect(document.querySelector('textarea')).toBeNull()
})

it('falls back when native access is denied and restores focus', async () => {
  vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
  const command = vi.fn().mockReturnValue(true)
  Object.defineProperty(document, 'execCommand', { configurable: true, value: command })
  const button = document.createElement('button')
  document.body.appendChild(button)
  button.focus()
  await copyText('invite')
  expect(command).toHaveBeenCalledWith('copy')
  expect(document.querySelector('textarea')).toBeNull()
  expect(document.activeElement).toBe(button)
})

it('reports rejected fallback copies and always removes its input', async () => {
  vi.stubGlobal('navigator', {})
  Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn().mockReturnValue(false) })
  await expect(copyText('invite')).rejects.toThrow('Clipboard copy rejected')
  expect(document.querySelector('textarea')).toBeNull()
})
