import { afterEach, describe, expect, it, vi } from 'vitest'
import { downloadCsv, toCsv } from '@/utils/csv'

describe('toCsv', () => {
  const columns = [{ key: 'name', label: '姓名' }, { key: 'note', label: '备注' }]

  it('writes a BOM, header and escaped cells', () => {
    const csv = toCsv(columns, [{ name: '张三', note: 'a,"b"\nc' }])
    expect(csv).toBe('﻿姓名,备注\r\n张三,"a,""b""\nc"')
  })

  it('neutralises formulas preceded by whitespace', () => {
    expect(toCsv(columns, [{ name: '  =1+1', note: 'ok' }])).toContain("\r\n'  =1+1,ok")
  })

  it('preserves date, bigint and custom string values without enabling formulas', () => {
    const date = new Date('2020-01-01T00:00:00Z')
    const csv = toCsv([{ key: 'value', label: 'value' }], [
      { value: date }, { value: 9007199254740993n },
      { value: { toString: () => '=1+1' } }
    ])
    expect(csv).toContain(String(date))
    expect(csv).toContain('9007199254740993')
    expect(csv).toContain("'=1+1")
  })
  it('neutralises spreadsheet formulas and tolerates missing values', () => {
    const csv = toCsv(columns, [{ name: '=HYPERLINK("x")', note: null }, { name: '-1' }])
    expect(csv.split('\r\n').slice(1)).toEqual(['"\'=HYPERLINK(""x"")",', "'-1,"])
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('downloadCsv resource lifetime', () => {
  const columns = [{ key: 'name', label: 'Name' }]
  it('keeps the blob alive while the browser begins the download, then releases it', () => {
    const revoke = vi.fn()
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:csv-export'), revokeObjectURL: revoke })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    vi.useFakeTimers()
    const before = document.querySelectorAll('a').length

    downloadCsv('people.csv', columns, [{ name: 'Alice' }])

    expect(document.querySelectorAll('a')).toHaveLength(before)
    expect(revoke).not.toHaveBeenCalled()
    vi.advanceTimersByTime(29_999)
    expect(revoke).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:csv-export')
  })

  it('removes the link and revokes its URL if the browser refuses the click', () => {
    const revoke = vi.fn()
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:failed-export'), revokeObjectURL: revoke })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => { throw new Error('download blocked') })
    const before = document.querySelectorAll('a').length

    expect(() => downloadCsv('people.csv', columns, [])).toThrow('download blocked')
    expect(document.querySelectorAll('a')).toHaveLength(before)
    expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:failed-export')
  })
})
