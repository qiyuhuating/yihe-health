import { afterEach, describe, expect, it, vi } from 'vitest'
import axios, { CanceledError } from 'axios'
import fs from 'node:fs'
import path from 'node:path'
import { SRC, sourceFiles } from '../helpers/sourceTree'
import { api, REQUEST_TIMEOUT_MS } from '@/api/client'
import { apiErrorMessage, transportOutcome, UNCONFIRMED_MESSAGE } from '@/api/httpContract'
import { endSession, installHttpSecurity } from '@/auth'

const originalTimeout = axios.defaults.timeout
const originalCredentials = axios.defaults.withCredentials

afterEach(() => {
  vi.restoreAllMocks()
  axios.interceptors.request.clear()
  axios.interceptors.response.clear()
  axios.defaults.timeout = originalTimeout
  axios.defaults.withCredentials = originalCredentials
})

type PostConfig = { timeout?: number; withCredentials?: boolean; signal?: AbortSignal; skipSession?: boolean }
const configAt = (calls: unknown[][], index: number): PostConfig =>
  (calls[index][2] ?? {}) as PostConfig

describe('application transport deadline', () => {
  it('gives every typed post a bounded wait and keeps cancellation', async () => {
    const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
    const controller = new AbortController()
    void api.post('api/v1/guardians/create', {}, { signal: controller.signal })
    const calls = post.mock.calls as unknown as unknown[][]
    expect(calls[0][0]).toBe('/api/v1/guardians/create')
    expect(configAt(calls, 0)).toMatchObject({ withCredentials: true, timeout: REQUEST_TIMEOUT_MS, signal: controller.signal })
  })

  it('allows a longer explicit wait but never an unbounded one', () => {
    const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
    void api.post('api/v1/guardians/create', {}, { timeoutMs: 30_000 })
    void api.post('api/v1/guardians/create', {}, { timeoutMs: 0 })
    void api.post('api/v1/guardians/create', {}, { timeoutMs: -1 })
    const calls = post.mock.calls as unknown as unknown[][]
    expect([0, 1, 2].map(index => configAt(calls, index).timeout))
      .toEqual([30_000, REQUEST_TIMEOUT_MS, REQUEST_TIMEOUT_MS])
  })

  it('sets a global default for the remaining direct calls', () => {
    expect(axios.defaults.timeout).not.toBe(REQUEST_TIMEOUT_MS)
    installHttpSecurity({ currentRoute: { value: { path: '/sign-in' } } })
    expect(axios.defaults.timeout).toBe(REQUEST_TIMEOUT_MS)
    expect(axios.defaults.withCredentials).toBe(true)
  })

  it('keeps the session-free local logout bounded', () => {
    const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
    void endSession()
    const calls = post.mock.calls as unknown as unknown[][]
    expect(configAt(calls, 0)).toMatchObject({ skipSession: true, timeout: REQUEST_TIMEOUT_MS })
  })
})

describe('unconfirmed request handling', () => {
  it('classifies transport outcomes', () => {
    expect(transportOutcome({ code: 'ECONNABORTED' })).toBe('timeout')
    expect(transportOutcome({ code: 'ETIMEDOUT' })).toBe('timeout')
    expect(transportOutcome({ code: 'ERR_NETWORK' })).toBe('network')
    expect(transportOutcome({ code: 'ECONNREFUSED' })).toBe('network')
    expect(transportOutcome({ response: { status: 500 } })).toBe('server')
    expect(transportOutcome(new CanceledError())).toBe('cancelled')
    expect(transportOutcome(new Error('boom'))).toBe('application')
  })

  it('never invites a blind retry after a lost response', () => {
    expect(UNCONFIRMED_MESSAGE).toContain('核对')
    expect(apiErrorMessage({ code: 'ECONNABORTED' }, '保存失败，请重试')).toBe(UNCONFIRMED_MESSAGE)
    expect(apiErrorMessage({ code: 'ERR_NETWORK' }, '保存失败，请重试')).toBe(UNCONFIRMED_MESSAGE)
  })

})

// Extract whole calls with balanced parentheses so a multi-line config cannot
// hide a missing timeout. Source files are the only source of truth here: a new
// escape hatch fails this test without maintaining any allowlist.
function directPosts(source: string): string[] {
  const calls: string[] = []
  let cursor = 0
  while ((cursor = source.indexOf('axios.post(', cursor)) !== -1) {
    let depth = 0
    let end = cursor + 'axios.post'.length
    for (; end < source.length; end++) {
      const char = source[end]
      if (char === '(') depth++
      else if (char === ')') {
        depth--
        if (depth === 0) { end++; break }
      } else if (char === '"' || char === "'" || char === '`') {
        const quote = char
        end++
        while (end < source.length && source[end] !== quote) {
          if (source[end] === '\\') end++
          end++
        }
      }
    }
    calls.push(source.slice(cursor, end))
    cursor = end
  }
  return calls
}

describe('transport escape-hatch guard', () => {
  it('requires an explicit timeout on every direct axios.post in src', () => {
    const files = sourceFiles(SRC)
    expect(files.length).toBeGreaterThan(0)
    const violations: string[] = []
    for (const file of files) {
      for (const call of directPosts(fs.readFileSync(file, 'utf8'))) {
        if (!/\btimeout\b/.test(call)) violations.push(`${path.relative(SRC, file)}: ${call.slice(0, 90)}`)
      }
    }
    expect(violations).toEqual([])
  })
})
