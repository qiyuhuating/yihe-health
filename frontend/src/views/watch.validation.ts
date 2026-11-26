import type { FormItemRule } from 'element-plus'

/** Shared Element Plus validator for physiological threshold values. */
export function checkThreshold(
  _rule: FormItemRule,
  value: unknown,
  callback: (error?: Error) => void
): void {
  if (value === '' || value == null) {
    callback(new Error('请输入阈值'))
    return
  }
  const number = Number(value)
  callback(Number.isInteger(number) && number >= 1 && number <= 300
    ? undefined : new Error('阈值须为1-300的整数'))
}
