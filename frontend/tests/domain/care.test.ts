import { describe, expect, it } from 'vitest'
import {
  deliveryLabel, isOverdue, nextStep, priorityClass, priorityLabel, stateLabel,
  type CareTask
} from '@/domain/care'

const task = (state: string, kind = 'check', severity = 'normal'): CareTask =>
  ({ id: 1, state, kind, severity })

describe('care incident presentation contract', () => {
  it('keeps notification delivery separate from human care completion', () => {
    expect(deliveryLabel('delivered')).toBe('已确认送达')
    expect(stateLabel('notified')).toBe('通知已送达')
    expect(stateLabel('verified')).toBe('已复核关闭')
    expect(nextStep(task('notified'))).toContain('接单')
    expect(nextStep(task('resolved'))).toContain('独立复核')
  })

  it('prioritizes SOS and falls above normal-severity records', () => {
    expect(priorityLabel(task('received', 'sos'))).toBe('P0 紧急')
    expect(priorityLabel(task('received', 'fall'))).toBe('P0 紧急')
    expect(priorityClass(task('received', 'other', 'high'))).toBe('priority-p1')
    expect(priorityLabel(task('received', 'other', 'normal'))).toBe('P2 常规')
  })

  it('keeps unknown protocol values visible and calculates overdue by deadline', () => {
    expect(stateLabel('future_state')).toBe('future_state')
    expect(deliveryLabel(undefined)).toBe('未确认')
    expect(nextStep(task('future_state'))).toBe('核对任务状态')
    expect(isOverdue({ ...task('received'), dueAt: '2020-01-01T00:00:00Z' })).toBe(true)
    expect(isOverdue(task('received'))).toBe(false)
  })
})
