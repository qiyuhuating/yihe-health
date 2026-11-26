import { AxiosError, isCancel, type AxiosResponse } from 'axios'
import statusErrors from './status-errors.json'

export interface ApiFailure {
  returnCode: '0000'
  code: string
  message: string
  reference?: string
}

export function isSuccessEnvelope(value: unknown): value is { returnCode: '1111' } {
  return value !== null && typeof value === 'object' && !Array.isArray(value) &&
    (value as { returnCode?: unknown }).returnCode === '1111'
}

export function isApplicationAPI(url: string | undefined): boolean {
  if (!url) return false
  try {
    const target = new URL(url, window.location.href)
    return target.origin === window.location.origin && target.pathname.startsWith('/api/v1/')
  } catch {
    return false
  }
}

export type TransportOutcome = 'cancelled' | 'timeout' | 'network' | 'server' | 'application'

// A lost response is not a safe retry: the server may already have applied
// the request. Never tell the user to retry without telling them to check.
export const UNCONFIRMED_MESSAGE = '未在时限内收到服务端确认，请求可能已被处理。请先核对结果，确认后再重试，避免重复操作。'

export function transportOutcome(error: unknown): TransportOutcome {
  if (isCancel(error)) return 'cancelled'
  const failure = error as { code?: unknown; response?: unknown; name?: unknown; status?: unknown } | null
  if (failure?.response) return 'server'
  const code = typeof failure?.code === 'string' ? failure.code : ''
  if (code === 'ECONNABORTED' || code === 'ETIMEDOUT') return 'timeout'
  if (['ERR_NETWORK', 'ECONNREFUSED', 'ENOTFOUND', 'EAI_AGAIN'].includes(code)) return 'network'
  // el-upload uses a raw XHR, so a lost response never reaches the axios
  // interceptors: it arrives as UploadAjaxError with status 0 and no payload.
  if (failure?.name === 'UploadAjaxError' && (failure.status === 0 || failure.status === undefined)) return 'network'
  return 'application'
}

export const READ_TIMEOUT_MESSAGE = '查询超时，请稍后重试'

// Reads have no side effects: a lost response here must not be described as a
// possibly-applied write. Keep the classification on the error, not on copy.
export function readTimeoutError(): Error & { code: string } {
  return Object.assign(new Error('read timed out'), { code: 'ERR_READ_TIMEOUT' })
}

export function readErrorMessage(error: unknown, fallback = '数据暂时无法加载'): string {
  if ((error as { code?: unknown } | null)?.code === 'ERR_READ_TIMEOUT') return READ_TIMEOUT_MESSAGE
  const outcome = transportOutcome(error)
  if (outcome === 'timeout') return READ_TIMEOUT_MESSAGE
  if (outcome === 'network') return '查询时网络连接中断，请检查网络后重试'
  return apiErrorMessage(error, fallback)
}

export function apiErrorMessage(error: unknown, fallback = '请求未完成'): string {
  if (isCancel(error)) return '请求已取消'
  const outcome = transportOutcome(error)
  if (outcome === 'timeout' || outcome === 'network') return UNCONFIRMED_MESSAGE
  const failure = error as { response?: { data?: Partial<ApiFailure>; status?: number }; name?: string; message?: string; status?: number; cause?: Partial<ApiFailure> }
  let data = failure?.response?.data || (failure?.cause?.returnCode === '0000' ? failure.cause : undefined)
  if (!data && failure?.name === 'UploadAjaxError' && failure.message) {
    try { data = JSON.parse(failure.message) as Partial<ApiFailure> } catch { /* Plain upload transport error. */ }
  }
  if (data?.returnCode === '0000' && typeof data.code === 'string' && typeof data.message === 'string' && data.message.trim()) {
    return data.message + (typeof data.reference === 'string' && data.reference ? `（编号：${data.reference}）` : '')
  }
  const status = failure?.response?.status || failure?.status
  const definition = statusErrors[String(status) as keyof typeof statusErrors]
  return definition?.message || fallback
}

export function assertApiResponse(response: AxiosResponse): AxiosResponse {
  if (!isApplicationAPI(response.config.url) || ['blob', 'arraybuffer'].includes(response.config.responseType || '')) return response
  if (isSuccessEnvelope(response.data)) return response
  const body: unknown = response.data
  const message = body !== null && typeof body === 'object' && 'returnCode' in body && body.returnCode === '0000'
    ? apiErrorMessage({ response }, '请求被拒绝')
    : '服务响应格式错误'
  throw new AxiosError(message, 'ERR_BAD_RESPONSE', response.config, response.request, response)
}
