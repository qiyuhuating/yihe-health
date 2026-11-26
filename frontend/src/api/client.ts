import axios from 'axios'
import { isSuccessEnvelope } from './httpContract'

export interface Envelope { returnCode: '1111' | '0000'; code?: string; message?: string; reference?: string }
export interface PostOptions { signal?: AbortSignal; timeoutMs?: number }
export interface PostClient { post<T>(path: string, body: unknown, options?: PostOptions): Promise<{ data: T }> }

// A transport deadline is the only recovery path for an unacknowledged write:
// aborting client-side cannot tell whether the server already committed it.
// Reads own their shorter 10s budget in createLatestRead; 15s stays below
// the server's 35s WriteTimeout so the UI can settle while a slow server
// may still finish.
export const REQUEST_TIMEOUT_MS = 15_000

const API_ORIGIN = 'https://yihe.invalid'

export function apiPath(path: string): string {
  if (typeof path !== 'string' || path.trim() === '') throw new Error('Invalid application API path')
  const candidate = path.startsWith('/') ? path : `/${path}`
  if (/[?#\\]/.test(candidate) || /%(?:2f|5c)/i.test(candidate)) throw new Error('Invalid application API path')
  const resolved = new URL(candidate, API_ORIGIN)
  if (resolved.origin !== API_ORIGIN || resolved.pathname !== candidate || !resolved.pathname.startsWith('/api/v1/')) {
    throw new Error('Invalid application API path')
  }
  return resolved.pathname
}

export const api: PostClient = {
  post: <T>(path: string, body: unknown, options?: PostOptions) => axios.post<T>(apiPath(path), body, {
    withCredentials: true,
    signal: options?.signal,
    timeout: options?.timeoutMs && options.timeoutMs > 0 ? options.timeoutMs : REQUEST_TIMEOUT_MS
  })
}
export function accepted(data: Envelope): void {
  if (!isSuccessEnvelope(data)) throw new Error(data?.message || '请求被拒绝', { cause: data })
}
