import { describe, expect, it } from 'vitest'
import type { LatestHealthRecord } from '@/api/health'
import { buildDailyCareDigest, formatElapsedZh } from '@/domain/dailyCareDigest'

function sample(overrides: Partial<LatestHealthRecord['sample']> = {}): LatestHealthRecord {
  return {
    sampleId: 's-1', deviceId: 1, bindingVersion: 1,
    sample: {
      recordId: 'r-1', source: 'yihe-harmonyos', sourceDevice: 'WATCH-01',
      metric: 'heart_rate', semantics: 'instant', unit: 'count/min', value: 74,
      start: '2026-11-24T01:00:00Z', end: '2026-11-24T01:00:00Z',
      version: 1, timeBasis: 'device', ...overrides
    }
  } as unknown as LatestHealthRecord
}

const now = Date.parse('2026-11-24T01:20:00Z')

describe('dailyCareDigest', () => {
  it('never claims missing samples mean normal health', () => {
    const result = buildDailyCareDigest([], now)
    expect(result.status).toBe('empty')
    expect(result.summary).toContain('不代表')
  })

  it('labels freshness as sample time, never as successful sync', () => {
    const result = buildDailyCareDigest([
      sample(), sample({ metric: 'steps', unit: 'count', value: 3200,
        start: '2026-11-24T01:10:00Z', end: '2026-11-24T01:10:00Z' })
    ], now)
    expect(result.status).toBe('calm')
    expect(result.freshnessText).toBe('10 分钟前采样')
    expect(result.summary).toContain('心率 74 次/分')
    expect(result.summary).toContain('步数 3200 步')
    expect(result.summary).toContain('不构成健康状况判断')
    expect(result.spokenText).toContain('10 分钟前采样')
  })

  it('flags old valid samples without asserting the watch is offline', () => {
    const result = buildDailyCareDigest([sample()], now + 8 * 3600_000)
    expect(result.status).toBe('stale')
    expect(result.freshnessText).toBe('8 小时前采样')
    expect(result.suggestedAction).toContain('不能据此判断设备当前离线')
  })

  it('refuses to guess freshness from unzoned, future or invalid timestamps', () => {
    for (const item of [
      sample({ timeBasis: 'legacy-unzoned', start: '2026-11-24 01:00:00', end: '2026-11-24 01:00:00' }),
      sample({ start: '2026-11-25T01:00:00Z', end: '2026-11-25T01:00:00Z' }),
      sample({ start: 'invalid', end: 'invalid' }),
      sample({ start: '2026-11-24 01:00:00', end: '' })
    ]) {
      const result = buildDailyCareDigest([item], now)
      expect(result.status).toBe('unknown')
      expect(result.freshnessText).toBe('无法确定采样新鲜度')
    }
  })

  it('updates status as time passes, even when record array stays unchanged', () => {
    const records = [sample()]
    expect(buildDailyCareDigest(records, now).status).toBe('calm')
    expect(buildDailyCareDigest(records, now + 7 * 3600_000).status).toBe('stale')
  })

  it('formats elapsed durations', () => {
    expect(formatElapsedZh(20_000)).toContain('接近当前')
    expect(formatElapsedZh(5 * 60_000)).toBe('5 分钟前采样')
    expect(formatElapsedZh(48 * 3600_000)).toBe('2 天前采样')
    expect(formatElapsedZh(-100)).toBe('采样时间未确认')
  })
})
