import type { FormItemRule } from 'element-plus'

export const PHONE_REGEX = /^1\d{10}$/
export const MFA_OTP_REGEX = /^\d{6}$/
export const PASSWORD_BYTE_LIMIT = 72

export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export const validatePhone: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  const normalized = typeof value === 'string' ? value.trim() : ''
  if (!normalized) return callback(new Error('请输入手机号'))
  if (!PHONE_REGEX.test(normalized)) return callback(new Error('请输入有效的 11 位手机号'))
  callback()
}

export const validatePasswordByteLimit: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (typeof value === 'string' && utf8ByteLength(value) <= PASSWORD_BYTE_LIMIT) {
    return callback()
  }
  callback(new Error(`密码不能超过 ${PASSWORD_BYTE_LIMIT} 个字节`))
}

export const validatePasswordComplexity: NonNullable<FormItemRule['validator']> = (_rule, value, callback) => {
  if (typeof value !== 'string' || !/[a-zA-Z]/.test(value) || !/\d/.test(value)) {
    return callback(new Error('密码必须同时包含字母和数字'))
  }
  callback()
}

export function validateMfaCredentials(otp: string, recoveryCode: string): string {
  const trimmedOtp = otp.trim()
  const trimmedRecovery = recoveryCode.trim()
  if (!trimmedOtp && !trimmedRecovery) return '请输入 6 位动态验证码或一次性恢复码'
  if (trimmedOtp && !MFA_OTP_REGEX.test(trimmedOtp)) return '动态验证码应为 6 位数字'
  return ''
}
