import { apiErrorMessage, transportOutcome } from './httpContract'

// Creating the first administrator is a one-shot write. A lost response must
// never be described as a validation failure, or the user will submit again.
export const SETUP_UNCONFIRMED_MESSAGE =
  '创建结果未能确认，账号可能已经建立。请勿重复提交，先用该手机号尝试登录；仍无法登录请联系管理员核对。'
export const SETUP_AFTER_CREATE_MESSAGE =
  '账号已创建，但自动登录未完成。请用刚设置的密码登录，不要重复创建账号。'
export const SETUP_REJECTED_FALLBACK = '暂时无法创建，请检查填写的信息后重试。'

export function setupFailureMessage(accountCreated: boolean, cause: unknown): string {
  if (accountCreated) return SETUP_AFTER_CREATE_MESSAGE
  const outcome = transportOutcome(cause)
  if (outcome === 'timeout' || outcome === 'network') return SETUP_UNCONFIRMED_MESSAGE
  return apiErrorMessage(cause, SETUP_REJECTED_FALLBACK)
}
