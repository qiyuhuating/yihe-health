import { describe, expect, it } from 'vitest'
import { CanceledError } from 'axios'
import {
  SETUP_AFTER_CREATE_MESSAGE,
  SETUP_REJECTED_FALLBACK,
  SETUP_UNCONFIRMED_MESSAGE,
  setupFailureMessage
} from '@/api/setupFailure'

const timeout = Object.assign(new Error('timeout'), { code: 'ECONNABORTED' })
const network = Object.assign(new Error('offline'), { code: 'ERR_NETWORK' })
const rejected = {
  response: { status: 400, data: { returnCode: '0000', code: 'BAD_PHONE', message: '手机号已被注册' } }
}
const cancelled = new CanceledError()

describe('one-shot administrator setup failures', () => {
  it.each([timeout, network])('never invites a replay when the creation result was lost', cause => {
    expect(setupFailureMessage(false, cause)).toBe(SETUP_UNCONFIRMED_MESSAGE)
    expect(setupFailureMessage(false, cause)).not.toContain('检查填写的信息')
  })

  it('surfaces an explicit server rejection unchanged', () => {
    expect(setupFailureMessage(false, rejected)).toBe('手机号已被注册')
  })

  it('falls back to validation copy only for a confirmed, unclassified failure', () => {
    expect(setupFailureMessage(false, new Error('unknown'))).toBe(SETUP_REJECTED_FALLBACK)
  })

  it('reports an explicit cancellation without implying an unconfirmed write', () => {
    expect(setupFailureMessage(false, cancelled)).not.toContain('可能已经建立')
  })

  it.each([timeout, network, rejected, new Error('session missing')])(
    'never suggests creating the account again once it already exists', cause => {
      const message = setupFailureMessage(true, cause)
      expect(message).toBe(SETUP_AFTER_CREATE_MESSAGE)
      expect(message).not.toContain('检查填写的信息')
      expect(message).not.toContain('重试')
    }
  )
})
