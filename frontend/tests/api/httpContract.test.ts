import { expect, it } from 'vitest'
import { CanceledError, type AxiosResponse } from 'axios'
import { apiErrorMessage, readErrorMessage, assertApiResponse, isApplicationAPI, isSuccessEnvelope } from '@/api/httpContract'
import { accepted, apiPath } from '@/api/client'

const response = (data: unknown) => ({ data, status: 200, config: { url: '/api/v1/guardians/list' } }) as AxiosResponse

it('accepts the common success envelope and rejects missing or failed results', () => {
  const success = response({ returnCode: '1111', customers: [], total: 0 })
  expect(assertApiResponse(success)).toBe(success)
  expect(() => assertApiResponse(response({ customers: [] }))).toThrow('服务响应格式错误')
  expect(() => assertApiResponse(response({ returnCode: '0000', code: 'CONFLICT', message: '绑定已变化' }))).toThrow('绑定已变化')
})

it('retains server messages, reference identifiers, HTTP definitions and cancellation', () => {
  expect(apiErrorMessage({ response: { status: 500, data: { returnCode: '0000', code: 'INTERNAL_ERROR', message: '保存失败', reference: 'r-1' } } })).toBe('保存失败（编号：r-1）')
  expect(apiErrorMessage({ response: { status: 429 } })).toBe('请求过于频繁，请稍后重试')
  expect(apiErrorMessage(new Error('network'), '连接中断')).toBe('连接中断')
  expect(apiErrorMessage(new CanceledError())).toBe('请求已取消')
  expect(apiErrorMessage({ name: 'UploadAjaxError', status: 400, message: JSON.stringify({ returnCode: '0000', code: 'INVALID_REQUEST', message: '图片格式不支持' }) })).toBe('图片格式不支持')
  expect(apiErrorMessage(new Error('rejected', { cause: { returnCode: '0000', code: 'INVALID_REQUEST', message: '手机号已使用' } }))).toBe('手机号已使用')
})

it('leaves external responses and binary downloads to their own protocols', () => {
  expect(isApplicationAPI('https://other.example/api/v1/guardians/list')).toBe(false)
  expect(isApplicationAPI('http://[invalid/api/v1/guardians/list')).toBe(false)
  const malformed = { ...response('ok'), config: { url: 'http://[invalid/api/v1/a' } } as AxiosResponse
  expect(assertApiResponse(malformed)).toBe(malformed)
  const external = { ...response('ok'), config: { url: 'https://other.example/api/v1/a' } } as AxiosResponse
  expect(assertApiResponse(external)).toBe(external)
  const image = { ...response(new Blob()), config: { url: '/api/v1/image/download?name=x', responseType: 'blob' } } as AxiosResponse
  expect(assertApiResponse(image)).toBe(image)
})

it('shares one success predicate for HTTP responses and explicit write acceptance', () => {
  const ok = { returnCode: '1111' as const }
  expect(isSuccessEnvelope(ok)).toBe(true)
  expect(() => accepted(ok)).not.toThrow()
  expect(() => accepted({ returnCode: '0000', code: 'CONFLICT', message: '记录已变化' })).toThrow('记录已变化')
  expect(isSuccessEnvelope({ returnCode: '0000' })).toBe(false)
  expect(isSuccessEnvelope([])).toBe(false)
  expect(isSuccessEnvelope(null)).toBe(false)
  expect(() => assertApiResponse(response({ returnCode: '0000', code: 'CONFLICT', message: '记录已变化' }))).toThrow('记录已变化')
})

it('canonicalizes local Go API routes and rejects non-API destinations', () => {
  expect(apiPath('api/v1/guardians/list')).toBe('/api/v1/guardians/list')
  expect(apiPath('/api/v1/guardians/list')).toBe('/api/v1/guardians/list')
  expect(() => apiPath('https://external.example/api/v1/guardians/list')).toThrow()
  expect(() => apiPath('/deprecated/guardians/list')).toThrow()
  for (const path of [
    '//external.example/api/v1/guardians/list',
    '/api/v1/../auth/session',
    '/api/v1/%2e%2e/auth/session',
    '/api/v1/%2Fadmin',
    '/api/v1\\admin',
    '/api/v1/guardians/list?next=/admin',
    '/api/v1/guardians/list#fragment'
  ]) expect(() => apiPath(path)).toThrow()
})

it('allows safe read retry messaging without weakening ambiguous write warnings', () => {
  expect(readErrorMessage({ code: 'ERR_NETWORK' })).toContain('查询时网络连接中断')
  expect(readErrorMessage({ code: 'ETIMEDOUT' })).toBe('查询超时，请稍后重试')
  expect(apiErrorMessage({ code: 'ERR_NETWORK' })).toContain('先核对结果')
  expect(readErrorMessage({ response: { status: 403, data: { returnCode: '0000', code: 'ACCESS_DENIED', message: '无权查看' } } })).toBe('无权查看')
})
