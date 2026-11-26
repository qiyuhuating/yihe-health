import { describe, expect, it } from 'vitest'
import { createDeviceState, emptyDeviceAlert, isValidCount, sameDeviceRows } from '@/composables/deviceModels'

describe('device state ownership', () => {
  it('accepts only nonnegative safe integer counts from device APIs', () => {
    expect([0, 1, 20].every(isValidCount)).toBe(true)
    expect([-1, NaN, Infinity, 1.5, '2', Number.MAX_SAFE_INTEGER + 1].some(isValidCount)).toBe(false)
  })

  it('allocates independent list, alert and dialog state for each device screen', () => {
    const first = createDeviceState()
    const second = createDeviceState()
    first.deviceList.push({ deviceId: 7, deviceNo: 'watch-7' })
    first.alertCounts['7'] = { personCount: 1, rateCount: 0, preCount: 0 }
    first.alertDialog.visible = true
    first.createForm.alias = 'edited'
    expect(second.deviceList).toEqual([])
    expect(second.alertCounts).toEqual({})
    expect(second.alertDialog.visible).toBe(false)
    expect(second.createForm.alias).toBe('')
    expect(emptyDeviceAlert()).toEqual(second.alertDialog)
  })
})

describe('scalar device polling comparisons', () => {
  const first = { deviceId: 1, deviceNo: 'A', battery: 80, online: 1 }
  const second = { deviceId: 2, deviceNo: 'B', battery: 75, online: 0 }

  it('keeps the existing row references when snapshot fields are unchanged', () => {
    expect(sameDeviceRows([first, second], [{ ...first }, { ...second }])).toBe(true)
    expect(sameDeviceRows([], [])).toBe(true)
  })

  it('detects changed fields, row ordering and added API fields', () => {
    expect(sameDeviceRows([first], [{ ...first, battery: 79 }])).toBe(false)
    expect(sameDeviceRows([first, second], [second, first])).toBe(false)
    const expanded = { ...first, extraField: 'new' }
    expect(sameDeviceRows([first], [expanded])).toBe(false)
    expect(sameDeviceRows([first], [{ deviceId: 1, deviceNo: 'A', battery: 80 }])).toBe(false)
  })
})
