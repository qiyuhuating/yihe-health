// Domain inputs are structural values, independent of API client modules.
export interface CareDigestRecord {
  sample: {
    start: string
    end?: string | null
    timeBasis?: string
    metric: string
    unit: string
    value: unknown
  }
}

export type DigestStatus = 'calm' | 'stale' | 'empty' | 'unknown'

export interface DailyCareDigest {
  status: DigestStatus
  headline: string
  summary: string
  freshnessText: string
  suggestedAction: string
  spokenText: string
}

const STALE_THRESHOLD_MS = 6 * 60 * 60 * 1000
const FUTURE_TOLERANCE_MS = 5 * 60 * 1000

const METRIC_LABELS: Record<string, string> = {
  heart_rate: '心率', blood_pressure_systolic: '收缩压',
  blood_pressure_diastolic: '舒张压', legacy_blood_pressure: '血压（旧协议）',
  oxygen_saturation: '血氧饱和度', body_temperature: '体温',
  body_mass: '体重', steps: '步数', sleep_duration: '睡眠时长'
}
const UNIT_LABELS: Record<string, string> = {
  'count/min': '次/分', count: '步', min: '分钟', degC: '℃',
  kg: '千克', mmHg: 'mmHg', '%': '%'
}

// These times are sample timestamps, NOT proof the device is online now.
export function formatElapsedZh(diffMs: number): string {
  if (!Number.isFinite(diffMs) || diffMs < 0) return '采样时间未确认'
  const minutes = Math.floor(diffMs / 60_000)
  if (minutes < 1) return '采样时间接近当前时间'
  if (minutes < 60) return `${minutes} 分钟前采样`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前采样`
  return `${Math.floor(hours / 24)} 天前采样`
}

function sampleTime(record: CareDigestRecord, nowMs: number): number | null {
  const sample = record.sample
  // The server marks legacy local timestamps as unzoned. Do not reinterpret
  // those strings in the operator's browser timezone.
  if (sample.timeBasis === 'legacy-unzoned') return null
  const raw = sample.end || sample.start
  // Only explicit ISO-8601 offsets or Z are safe to compare across timezones.
  if (typeof raw !== 'string' || !/(?:Z|[+-]\d{2}:\d{2})$/.test(raw)) return null
  const at = Date.parse(raw)
  if (!Number.isFinite(at) || at > nowMs + FUTURE_TOLERANCE_MS) return null
  return at
}

export function buildDailyCareDigest(
  records: readonly CareDigestRecord[], nowMs: number = Date.now()
): DailyCareDigest {
  if (!records.length) {
    const headline = '暂未收到健康采样'
    const summary = '没有新记录不代表长者身体状态正常。'
    const suggestedAction = '建议确认手表电量、佩戴和连接情况，必要时联系长者。'
    return { status: 'empty', headline, summary, freshnessText: '暂无采样', suggestedAction,
      spokenText: `${headline}。${summary}${suggestedAction}` }
  }

  let latestMs = -Infinity
  const highlights: string[] = []
  const seen = new Set<string>()
  for (const item of records) {
    const timestamp = sampleTime(item, nowMs)
    if (timestamp !== null && timestamp > latestMs) latestMs = timestamp
    const { metric, unit, value } = item.sample
    if (highlights.length < 3 && !seen.has(metric) && METRIC_LABELS[metric] &&
        (typeof value === 'string' || (typeof value === 'number' && Number.isFinite(value)))) {
      seen.add(metric)
      highlights.push(`${METRIC_LABELS[metric]} ${value}${unit ? ' ' + (UNIT_LABELS[unit] || unit) : ''}`)
    }
  }
  const summary = highlights.length
    ? `记录中包含${highlights.join('、')}。这些数值不构成健康状况判断。`
    : `目前有 ${records.length} 项采样记录；这些记录不构成健康状况判断。`

  if (!Number.isFinite(latestMs)) {
    const headline = '采样时间尚未确认'
    const suggestedAction = '请核对手表时间及上报记录，必要时联系长者。'
    return { status: 'unknown', headline, summary, freshnessText: '无法确定采样新鲜度', suggestedAction,
      spokenText: `${headline}。${summary}${suggestedAction}` }
  }

  const elapsed = Math.max(0, nowMs - latestMs)
  const freshnessText = formatElapsedZh(elapsed)
  if (elapsed > STALE_THRESHOLD_MS) {
    const headline = '最近一次可确认的采样已超过 6 小时'
    const suggestedAction = '建议核对手表状态并联系长者确认；不能据此判断设备当前离线。'
    return { status: 'stale', headline, summary, freshnessText, suggestedAction,
      spokenText: `${headline}。${freshnessText}。${summary}${suggestedAction}` }
  }

  const headline = '已收到近期健康采样'
  const suggestedAction = '如需查看趋势与异常告警，请打开健康记录和告警页面。'
  return { status: 'calm', headline, summary, freshnessText, suggestedAction,
    spokenText: `${headline}。${freshnessText}。${summary}${suggestedAction}` }
}
